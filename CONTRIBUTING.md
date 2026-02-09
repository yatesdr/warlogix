# Contributing to Warlink

## Bug Reports

Bug reports are welcome and encouraged. If you encounter an issue, please open a GitHub issue with:

- A clear description of the problem
- Steps to reproduce the issue
- Expected vs actual behavior
- Your environment (OS, Go version, PLC type, etc.)
- Relevant log output or error messages (see below)

### Generating Useful Debug Logs

Enable debug logging with the `--log-debug` flag to capture protocol-level details. Output is written to `debug.log` in the working directory.

```bash
# All protocols (very verbose)
./warlink --log-debug

# Specific protocol
./warlink --log-debug=logix

# Multiple protocols
./warlink --log-debug=s7,mqtt,kafka
```

Available filters: `logix`, `s7`, `ads`, `omron`, `mqtt`, `kafka`, `valkey`, `tui`

When filing a bug report, include the relevant portion of `debug.log` with any sensitive information redacted. For complete documentation on debug logging and troubleshooting, see [docs/troubleshooting.md](docs/troubleshooting.md).

## Pull Requests

**Pull requests will not be accepted at this time.**

This project is currently single-author and the license framework has not yet been decided. Until these matters are resolved, all PRs will be closed without review.

## Feature Requests

Feature requests may be submitted as GitHub issues. While implementation contributions cannot be accepted, feedback on desired functionality is appreciated and may inform future development.

## Questions

For questions about usage or configuration, please open a GitHub issue with the "question" label.
