package harnas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrInvalidUnicode = errors.New("invalid_unicode")

func CanonicalizeJCSV1JSON(data []byte, excludeKeys ...string) ([]byte, error) {
	if err := validateSurrogateEscapes(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if len(excludeKeys) > 0 {
		if object, ok := value.(map[string]any); ok {
			for _, key := range excludeKeys {
				delete(object, key)
			}
		}
	}
	var out bytes.Buffer
	if err := writeJCSV1(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ContentHashForEventRowJSON(data []byte) (string, error) {
	canonical, err := CanonicalizeJCSV1JSON(data, "content_hash")
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func ContentHashForEventRow(row EventRow) (string, error) {
	object := map[string]any{
		"seq":       row.Seq,
		"id":        row.ID,
		"timestamp": row.Timestamp,
		"type":      string(row.Type),
		"payload":   row.Payload,
	}
	var out bytes.Buffer
	if err := writeJCSV1(&out, object); err != nil {
		return "", err
	}
	digest := sha256.Sum256(out.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func writeJCSV1(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		if !utf8.ValidString(v) {
			return ErrInvalidUnicode
		}
		writeJCSString(out, v)
	case json.Number:
		text, err := canonicalNumber(v.String())
		if err != nil {
			return err
		}
		out.WriteString(text)
	case float64:
		text, err := es6Number(v)
		if err != nil {
			return err
		}
		out.WriteString(text)
	case float32:
		text, err := es6Number(float64(v))
		if err != nil {
			return err
		}
		out.WriteString(text)
	case int:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int8:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int16:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int32:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int64:
		out.WriteString(strconv.FormatInt(v, 10))
	case uint:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint8:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint16:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint32:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint64:
		out.WriteString(strconv.FormatUint(v, 10))
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJCSV1(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case []map[string]any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJCSV1(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			if !utf8.ValidString(key) {
				return ErrInvalidUnicode
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return compareUTF16(keys[i], keys[j]) < 0
		})
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeJCSString(out, key)
			out.WriteByte(':')
			if err := writeJCSV1(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func canonicalNumber(raw string) (string, error) {
	if strings.ContainsAny(raw, ".eE") {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return "", err
		}
		return es6Number(f)
	}
	if raw == "-0" {
		return "0", nil
	}
	return raw, nil
}

func es6Number(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("invalid number")
	}
	if f == 0 {
		return "0", nil
	}
	raw := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(raw, "eE") {
		return raw, nil
	}
	mantissa, exponent, _ := strings.Cut(strings.ToLower(raw), "e")
	exp, err := strconv.Atoi(exponent)
	if err != nil {
		return "", err
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return exponentToDecimal(mantissa, exp), nil
	}
	return normalizeExponent(mantissa, exp), nil
}

func exponentToDecimal(mantissa string, exponent int) string {
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(mantissa, "-")
	digits := strings.ReplaceAll(mantissa, ".", "")
	decimalPlaces := 0
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		decimalPlaces = len(mantissa) - dot - 1
	}
	point := len(digits) - decimalPlaces + exponent
	var out string
	switch {
	case point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		out = digits + strings.Repeat("0", point-len(digits))
	default:
		out = digits[:point] + "." + digits[point:]
	}
	out = strings.TrimRight(out, "0")
	out = strings.TrimRight(out, ".")
	if negative {
		return "-" + out
	}
	return out
}

func normalizeExponent(mantissa string, exponent int) string {
	if exponent >= 0 {
		return mantissa + "e+" + strconv.Itoa(exponent)
	}
	return mantissa + "e" + strconv.Itoa(exponent)
}

func writeJCSString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteString("0123456789abcdef"[byte(r)>>4 : byte(r)>>4+1])
				out.WriteString("0123456789abcdef"[byte(r)&0x0f : byte(r)&0x0f+1])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func compareUTF16(a, b string) int {
	left := utf16.Encode([]rune(a))
	right := utf16.Encode([]rune(b))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

func validateSurrogateEscapes(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if b == '"' {
			inString = false
			continue
		}
		if b != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return nil
		}
		if data[i] != 'u' {
			continue
		}
		code, ok := readHex4(data, i+1)
		if !ok {
			continue
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(data) || data[i+5] != '\\' || data[i+6] != 'u' {
				return ErrInvalidUnicode
			}
			next, ok := readHex4(data, i+7)
			if !ok || next < 0xdc00 || next > 0xdfff {
				return ErrInvalidUnicode
			}
			i += 10
			continue
		}
		if code >= 0xdc00 && code <= 0xdfff {
			return ErrInvalidUnicode
		}
		i += 4
	}
	return nil
}

func readHex4(data []byte, start int) (rune, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value rune
	for _, b := range data[start : start+4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value += rune(b - '0')
		case b >= 'a' && b <= 'f':
			value += rune(b-'a') + 10
		case b >= 'A' && b <= 'F':
			value += rune(b-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
