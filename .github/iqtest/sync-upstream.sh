#!/usr/bin/env bash
set -euo pipefail

upstream=Wei-Shaw/sub2api
release=$(gh api "repos/$upstream/releases/latest")
tag=$(jq -er 'select(.draft == false and .prerelease == false) | .tag_name' <<<"$release")
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Unsupported upstream release tag: $tag" >&2
  exit 1
fi

# Never replace the custom branch or use the upstream development branch.
git fetch --no-tags "https://github.com/$upstream.git" "refs/tags/$tag:refs/iqtest-upstream/$tag"
upstream_sha=$(git rev-parse "refs/iqtest-upstream/$tag^{commit}")
if git merge-base --is-ancestor "$upstream_sha" HEAD && [[ $(cat .github/iqtest/upstream-version) == "$tag" ]]; then
  echo 'changed=false' >> "$GITHUB_OUTPUT"
  echo "Official $tag is already included. No deployment requested." >> "$GITHUB_STEP_SUMMARY"
  exit 0
fi

git config user.name 'github-actions[bot]'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
branch="automation/upstream-$tag-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
git checkout -b "$branch"
if ! git merge --no-ff --no-commit "$upstream_sha"; then
  {
    echo "## Official $tag requires conflict resolution"
    echo 'Production branch and VPS were not changed.'
    echo '```'
    git diff --name-only --diff-filter=U
    echo '```'
  } >> "$GITHUB_STEP_SUMMARY"
  git merge --abort
  exit 1
fi
printf '%s\n' "$tag" > .github/iqtest/upstream-version
git add .github/iqtest/upstream-version
git commit -m "Merge official $tag into IQTest edition"
git push origin "HEAD:refs/heads/$branch"
{
  echo 'changed=true'
  echo "sha=$(git rev-parse HEAD)"
  echo "branch=$branch"
} >> "$GITHUB_OUTPUT"
echo "Merged official $tag into $branch. Tests must pass before promotion and deployment." >> "$GITHUB_STEP_SUMMARY"
