#!/bin/sh
# Package a signed app bundle and an /Applications symlink into a compressed
# read-only disk image. This mirrors CopyQmac's release packaging: a staging
# folder holds the app and the symlink, and hdiutil creates the UDZO image.
set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <app-bundle> <output-dmg> <volume-name>" >&2
	exit 2
fi

bundle=$1
dmg=$2
volume=$3
stage=$(dirname "$dmg")/dmg-stage

if [ ! -d "$bundle" ]; then
	echo "missing app bundle: $bundle" >&2
	exit 1
fi

cleanup() {
	rm -rf "$stage"
}
trap cleanup EXIT

rm -rf "$stage" "$dmg"
mkdir -p "$stage" "$(dirname "$dmg")"
cp -R "$bundle" "$stage"/
ln -s /Applications "$stage/Applications"
hdiutil create -volname "$volume" -srcfolder "$stage" -ov -format UDZO "$dmg"
echo "Packaged $dmg"
