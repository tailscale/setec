#!/bin/bash
# RPM pre-removal script for leger
# Based on Tailscale's rpm.prerm.sh
#
# RPM scriptlet parameters:
# $1 == 0 for uninstallation
# $1 == 1 for removing old package during upgrade

if [ $1 -eq 0 ]; then
    # Package removal (not upgrade) - stop and disable services
    
    # Stop and disable system service
    systemctl --no-reload disable legerd.service >/dev/null 2>&1 || :
    systemctl stop legerd.service >/dev/null 2>&1 || :
fi

# For upgrades ($1 == 1), we don't stop the service
# It will be restarted by postrm after the new version is installed
# This matches Tailscale's pattern for seamless upgrades
