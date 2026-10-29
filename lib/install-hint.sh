#!/bin/sh
# install-hint.sh — print the command that installs a package
# on this workstation, one line, for messages such as "jq is
# missing". Reads uname and /etc/os-release only; installs
# nothing and needs no network.
#
#   sh lib/install-hint.sh jq
#
# The two variables below exist for the tests:
# JOLITZ_UNAME replaces `uname -s`, JOLITZ_OS_RELEASE names
# another os-release file.

pkg=${1:?usage: install-hint.sh <package>}
os=${JOLITZ_UNAME:-$(uname -s 2>/dev/null)}
rel=${JOLITZ_OS_RELEASE:-/etc/os-release}

# winget knows jq as jqlang.jq; every other manager as jq.
case "$os" in
  Darwin) echo "brew install $pkg"; exit 0 ;;
  FreeBSD) echo "pkg install $pkg"; exit 0 ;;
  NetBSD) echo "pkgin install $pkg"; exit 0 ;;
  OpenBSD) echo "pkg_add $pkg"; exit 0 ;;
  MINGW*|MSYS*|CYGWIN*)
    [ "$pkg" = jq ] && pkg=jqlang.jq
    echo "winget install $pkg"; exit 0 ;;
esac

# Linux: the distribution and the ones it is like.
ids=""
if [ -r "$rel" ]; then
  ids=$(sed -n -e 's/^ID=//p' -e 's/^ID_LIKE=//p' "$rel" \
    | tr -d '"' | tr '\n' ' ')
fi
for id in $ids; do
  case "$id" in
    debian|ubuntu) echo "sudo apt install $pkg"; exit 0 ;;
    fedora|rhel|centos|rocky|almalinux)
      echo "sudo dnf install $pkg"; exit 0 ;;
    suse|opensuse*|sles) echo "sudo zypper install $pkg"; exit 0 ;;
    alpine) echo "sudo apk add $pkg"; exit 0 ;;
    arch) echo "sudo pacman -S $pkg"; exit 0 ;;
  esac
done
echo "your package manager (package $pkg)"
