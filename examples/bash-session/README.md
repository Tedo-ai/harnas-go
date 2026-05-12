# bash_session example

This manifest exposes the experimental `harnas.builtin.bash_session`
tool to a live provider. The tool keeps a named shell session alive
across calls, so `cd`, exported environment variables, and long-running
processes can span turns.

```sh
export OPENAI_API_KEY=...
bin/harnas chat examples/bash-session/manifest.json
```

Try prompts such as:

- `Create a temporary directory, cd into it, and print the working directory.`
- `Export PROJECT_NAME=harnas-demo, then echo it in a second command.`
- `Start a short background sleep, check its status, then kill it.`

`bash_session` is experimental in harnas-go. The narrower built-ins
(`list_dir`, `glob`, `grep`, `run_shell`) remain available for agents
that should expose a smaller tool surface.
