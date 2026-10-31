#!/usr/bin/env bash
# Régénère les archives RAR de test. Demande rar 6.x (la 7.x ne crée plus de RAR 4) :
#   RAR=/chemin/vers/rar bash backend/archive/testdata/make.sh
set -euo pipefail
rar=${RAR:-rar}
out=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cd "$work"
mkdir S01
printf 'episode 1' >S01/Serie.S01E01.mkv
printf 'episode 2' >Serie.S01E02.mkv
printf 'readme' >readme.txt
files=(S01/Serie.S01E01.mkv Serie.S01E02.mkv readme.txt)

rm -f "$out"/rar4.rar "$out"/rar5.rar "$out"/rar5-headers.rar "$out"/rar5-files.rar
"$rar" a -idq -ma4 "$out/rar4.rar" "${files[@]}"
"$rar" a -idq -ma5 -s "$out/rar5.rar" "${files[@]}"
"$rar" a -idq -ma5 -hpfixture-pw "$out/rar5-headers.rar" "${files[@]}"
"$rar" a -idq -ma5 -pfixture-pw "$out/rar5-files.rar" "${files[@]}"
ls -l "$out"/*.rar
