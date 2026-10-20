#!/usr/bin/env bash
# Scaffold smoke: prove that scaffolded output passes the full gate untouched.
#
# The smoke copies this repository to a temporary directory, scaffolds a fresh
# feature and one allowlisted snapshot operation in the copy, regenerates the
# derived artifacts, commits them (the drift gate compares against a clean
# tree), and runs `make verify` there. The operation is a throwaway stand-in:
# the smoke cares that the templates are gate-clean, not about feature
# placement.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
feature="merchantonboarding"
operation="merchantOnboarding.referenceData.get"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "scaffold-smoke: copying the working tree to $workdir"
mkdir -p "$workdir/repo"
cp -a "$repo_root/." "$workdir/repo" || { echo "scaffold-smoke: copy failed" >&2; exit 1; }
cd "$workdir/repo"
rm -rf .cache

commit() {
	git add -A
	# A clean checkout has nothing to commit at the baseline, which is not a failure.
	if git diff --cached --quiet; then
		return 0
	fi
	git -c user.email=smoke@example.invalid -c user.name="scaffold smoke" commit -q -m "$1"
}

commit "smoke: baseline"
echo "scaffold-smoke: scaffolding feature $feature"
go run ./tools/gokit scaffold feature --name "$feature"
go run ./tools/gokit scaffold endpoint --feature "$feature" --operation "$operation"
echo "scaffold-smoke: regenerating derived artifacts"
make generate
commit "smoke: scaffolded output"
echo "scaffold-smoke: running the full gate on the scaffolded tree"
make verify
echo "scaffold-smoke: passed — scaffolded output is gate-clean"
