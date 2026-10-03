#!/usr/bin/env bash
set -euo pipefail

# Run inside Linux with the candidate mounted read-only at /src.
profile=${1:?usage: prepare.sh primary-gap|fallback-gap|missing-all}
case "$profile" in
  primary-gap|fallback-gap|missing-all) ;;
  *) printf 'Unknown locale profile: %s\n' "$profile" >&2; exit 2 ;;
esac
output=/receipts/locale-profile
mkdir "$output"
cd /src/platform
printf '%s\n' \
  '7db018e92d3ec8db092e8fcef2fc3b9506fea0a40bfeddbe1d237b4a898954d7  internal/i18n/catalog_en.go' \
  '2976ae412c63c52e5882dc67ec0beb2d4d09e6d4096a037fbe3d784d0792870d  internal/i18n/catalog_ru.go' |
  sha256sum --check --strict

printf '%s\n' '{"Replace":{' > "$output/overlay.json"
for locale in en ru; do
  source="internal/i18n/catalog_${locale}.go"
  if [[ "$profile" == missing-all || ( "$profile" == fallback-gap && "$locale" == ru ) ]]; then
    [[ $(grep -c '^[[:space:]]*LanguageChoose:' "$source") == 1 ]]
    sed '/^[[:space:]]*LanguageChoose:/d' "$source" > "$output/catalog_${locale}.go"
    printf '"/src/platform/%s":"%s/catalog_%s.go",\n' "$source" "$output" "$locale" >> "$output/overlay.json"
  fi
done
printf '%s\n' \
  '"/src/platform/internal/i18n/locale_profile_internal_test.go":"/src/platform/testdata/locale-profiles/check.go.txt"' \
  '}}' >> "$output/overlay.json"
printf '%s\n' "$profile" > "$output/profile.txt"
sha256sum "$output"/* > /receipts/locale-profile.sha256
