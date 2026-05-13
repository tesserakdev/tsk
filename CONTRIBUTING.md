# Contributing to tsk

Thanks for your interest in contributing. Here's everything you need to get started.

## Getting started

You'll need [Go 1.26+](https://go.dev/dl/) and [Task](https://taskfile.dev/installation/) installed.

```bash
git clone https://github.com/tesserakdev/tsk.git
cd tsk
task build   # builds ./bin/tsk
task test    # runs the test suite
task lint    # runs go vet
```

## Developer Certificate of Origin (DCO)

All commits must be signed off to certify that you wrote the code and have the right to submit it. Add `-s` to your commit command:

```bash
git commit -s -m "your message"
```

This appends a `Signed-off-by: Your Name <you@example.com>` line to the commit. By doing so you agree to the [DCO](https://developercertificate.org/).

## Signed commits

All commits must also carry a cryptographic signature. Configure Git to sign commits automatically:

```bash
git config --global commit.gpgsign true
```

If you haven't set up a signing key yet, GitHub's guide covers both [GPG keys](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key) and [SSH signing](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification#ssh-commit-signature-verification).

## Workflow

1. Fork the repo and create a branch from `main`.
2. Make your change — keep it focused; one concern per PR.
3. Add or update tests to cover your change.
4. Run `task test` and `task lint` — both must pass.
5. Sign and sign off every commit with `git commit -s -S` (see DCO and Signed commits above).
6. Open a pull request against `main` with a clear description of what and why.

## Code style

- Follow standard Go conventions. Use `gofmt`.
- Keep commits small and descriptive.
- Don't add comments that restate what the code does — only explain non-obvious *why*.

## Reporting bugs

Open an issue on GitHub. Include:
- What you ran
- What you expected
- What happened instead
- Your OS, Go version, and tsk version (`tsk --version`)

## Submitting features

For anything significant, open an issue first to discuss the idea before writing code. This avoids wasted effort if the direction doesn't fit the project.

## License

By contributing, you agree your code will be licensed under the [Apache 2.0 License](LICENSE).
