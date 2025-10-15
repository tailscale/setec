#!/bin/bash
# RPM post-removal script for leger
# Based on Tailscale's rpm.postrm.sh
#
# RPM scriptlet parameters:
# $1 == 0 for uninstallation
# $1 == 1 for removing old package during upgrade

# Always reload systemd daemon after package changes
systemctl daemon-reload >/dev/null 2>&1 || :

if [ $1 -ge 1 ]; then
    # Package upgrade, not uninstall
    # Restart the service if it was running
    # This is the seamless upgrade pattern
    systemctl try-restart legerd.service >/dev/null 2>&1 || :
fi

# For full uninstall ($1 == 0), services are already stopped by prerm
# Just inform the user about leftover data
if [ $1 -eq 0 ]; then
    echo ""
    echo "leger has been removed."
    echo "Data preserved in: /var/lib/leger"
    echo "To remove all data: sudo rm -rf /var/lib/leger /etc/leger"
    echo ""
fi
