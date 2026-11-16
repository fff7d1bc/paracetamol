#!/bin/sh
set -eu

# Go's own config is checkout-local, but tests must resolve application config
# exactly as they would when launched by the user, including unset versus empty.
if [ "${PARACETAMOL_TEST_XDG_CONFIG_HOME_SET-}" = x ]; then
    XDG_CONFIG_HOME=${PARACETAMOL_TEST_XDG_CONFIG_HOME-}
    export XDG_CONFIG_HOME
else
    unset XDG_CONFIG_HOME
fi
unset PARACETAMOL_TEST_XDG_CONFIG_HOME_SET PARACETAMOL_TEST_XDG_CONFIG_HOME
exec "$@"
