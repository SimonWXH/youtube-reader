#!/bin/sh
# Sign the built macOS app bundle and its nested Mach-O binaries without --deep.
# Uses a valid local code signing identity when present in the keychain, and
# falls back to ad-hoc signing (-) on CI runners or machines without the cert.
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <app-bundle>" >&2
	exit 2
fi

bundle=$1
requested_identity=${SIGN_IDENTITY:-YouTube Reader Local Signing}
codesign_bin=${CODESIGN:-codesign}
security_bin=${SECURITY:-security}
helper_id=${HELPER_BUNDLE_ID:-com.simon.youtubereader.helper}

if [ ! -d "$bundle" ]; then
	echo "missing app bundle: $bundle" >&2
	exit 1
fi

sign_target="-"
mode="ad-hoc"
resolved_name="-"
resolved_hash=""
fallback_reason=""

if [ -n "$requested_identity" ] && [ "$requested_identity" != "-" ]; then
	if command -v "$security_bin" >/dev/null 2>&1; then
		# Query Matching identities without -v so self-signed local certs without
		# system trust settings are still detected; stop before "Valid identities only"
		# to avoid counting a trusted identity twice.
		identities=$("$security_bin" find-identity -p codesigning 2>/dev/null || true)
		matches=$(printf '%s\n' "$identities" | REQ="$requested_identity" awk '
			/Valid identities only/ { exit }
			/^[[:space:]]*[0-9]+\)[[:space:]]+[0-9A-Fa-f]{40}[[:space:]]+"/ {
				hash = toupper($2)
				q1 = index($0, "\"")
				rest = substr($0, q1 + 1)
				q2 = index(rest, "\"")
				if (q2 > 0) {
					name = substr(rest, 1, q2 - 1)
					if ((name == ENVIRON["REQ"] || hash == toupper(ENVIRON["REQ"])) && !seen[hash]++) {
						print hash "\t" name
					}
				}
			}
		')
		match_count=$(printf '%s\n' "$matches" | awk 'NF { count++ } END { print count + 0 }')
		if [ "$match_count" -eq 1 ]; then
			resolved_hash=$(printf '%s\n' "$matches" | cut -f1)
			resolved_name=$(printf '%s\n' "$matches" | cut -f2)
			sign_target=$resolved_hash
			mode="local-identity"
		elif [ "$match_count" -gt 1 ]; then
			echo "ambiguous code signing identity \"$requested_identity\" ($match_count matches); pass a 40-char SHA-1 hash via SIGN_IDENTITY:" >&2
			printf '%s\n' "$matches" >&2
			exit 1
		else
			mode="ad-hoc-fallback"
			fallback_reason="no keychain code signing identity matches $requested_identity"
		fi
	else
		mode="ad-hoc-fallback"
		fallback_reason="security CLI unavailable"
	fi
fi

case "$mode" in
	local-identity)
		echo "Signing $bundle: mode=$mode identity=\"$resolved_name\" sha1=$resolved_hash invocation=nested-then-bundle (no --deep)"
		;;
	ad-hoc-fallback)
		echo "Signing $bundle: ad-hoc signed (identity -), no stable identity matched requested=\"$requested_identity\" reason=\"$fallback_reason\" invocation=nested-then-bundle (no --deep)"
		;;
	*)
		echo "Signing $bundle: ad-hoc signed (identity -, ad-hoc requested explicitly) invocation=nested-then-bundle (no --deep)"
		;;
esac

sign_nested() {
	target=$1
	shift
	if [ "$#" -gt 0 ]; then
		echo "  -> $codesign_bin --force --sign $sign_target $* $target"
	else
		echo "  -> $codesign_bin --force --sign $sign_target $target"
	fi
	"$codesign_bin" --force --sign "$sign_target" "$@" "$target"
}

resources_dir="$bundle/Contents/Resources"
helper_bin="$resources_dir/ytreader-helper"
if [ -f "$helper_bin" ]; then
	sign_nested "$helper_bin" --identifier "$helper_id"
fi

if [ -d "$resources_dir" ]; then
	for dylib in "$resources_dir"/*.dylib; do
		if [ -f "$dylib" ]; then
			sign_nested "$dylib"
		fi
	done
fi

echo "  -> $codesign_bin --force --sign $sign_target $bundle"
"$codesign_bin" --force --sign "$sign_target" "$bundle"
