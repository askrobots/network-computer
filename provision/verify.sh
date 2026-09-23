#!/bin/sh
# Check that a host has every piece of setup the network computer needs.
# Each line is a fix that was once done by hand; if it is not here, it was
# not captured in provisioning. Exits non-zero if anything fails.
#
#   sh provision/verify.sh          (run as root on the host; apply.sh runs it last)
export DISPLAY=:0
PASS=0; FAIL=0
check() {  # check "<what>" <command...>
  what=$1; shift
  if "$@" >/dev/null 2>&1; then printf '  ok    %s\n' "$what"; PASS=$((PASS+1))
  else printf '  FAIL  %s\n' "$what"; FAIL=$((FAIL+1)); fi
}
has() { grep -q "$2" "$1" 2>/dev/null; }

echo "services"
for s in nc-xorg nc-desktop nc-audio nc-rendezvous nc-host; do check "$s running" systemctl is-active --quiet "$s"; done

echo "display"
check "X answers on :0"                  xset q
check "screen blanking off"              sh -c 'xset q | grep -q "timeout:  0"'
check "uinput device available"          test -c /dev/uinput

echo "audio"
check "virtual sink 'nc' exists"         sh -c 'pactl list short sinks | grep -qP "^\d+\tnc\t"'
check "sink runs at 48 kHz"              sh -c 'pactl list short sinks | grep -P "\tnc\t" | grep -q 48000Hz'
check "'nc' is the default sink"         sh -c '[ "$(pactl get-default-sink)" = nc ]'
check "pulse socket at /run/nc-pulse"    test -S /run/nc-pulse/native
check "mic sink 'nc-mic' exists"          sh -c 'pactl list short sinks | grep -qP "^\d+\tnc-mic\t"'
check "phone mic is the default input"   sh -c '[ "$(pactl get-default-source)" = nc-mic-in ]'
check "host plays client mic (-mic-device)" sh -c 'systemctl cat nc-host | grep -q -- "-mic-device nc-mic"'
check "waveform launcher installed"      sh -c 'command -v ffplay && command -v nc-mic-scope && test -f /usr/share/applications/nc-mic-scope.desktop'
check "pavucontrol installed"            command -v pavucontrol
check "no pulse autospawn override"      sh -c '! test -e /etc/pulse/client.conf.d/01-enable-autospawn.conf'

echo "browser + default apps"
check "Firefox from Mozilla (not snap)"  sh -c 'dpkg-query -W -f="\${Version}" firefox | grep -q build'
check "https opens Firefox"              sh -c '[ "$(xdg-mime query default x-scheme-handler/https)" = firefox.desktop ]'
check "xfce WebBrowser helper = firefox" has /etc/xdg/xfce4/helpers.rc '^WebBrowser=firefox'
check "xdotool installed"                command -v xdotool

echo "desktop polish"
check "mousepad word wrap on"            sh -c '[ "$(gsettings get org.xfce.mousepad.preferences.view word-wrap)" = true ]'

echo "network"
check "IPv6 disabled (no route here)"    sh -c '[ "$(cat /proc/sys/net/ipv6/conf/all/disable_ipv6)" = 1 ]'
check "firewall allows 8765/tcp"         sh -c 'ufw status | grep -q "8765/tcp"'
check "firewall allows 3478/udp"         sh -c 'ufw status | grep -q "3478/udp"'

echo "network computer"
set -a; . /etc/nc/env 2>/dev/null; set +a   # export, so the checks below see them
check "rendezvous accepts the password"  sh -c 'curl -sf -o /dev/null -u "nc:$NC_PASSWORD" http://127.0.0.1:8765/hosts'
check "host name recorded in /etc/nc/env" test -n "$NC_HOST_NAME"
check "host registered as '$NC_HOST_NAME'" sh -c 'curl -sf -u "nc:$NC_PASSWORD" http://127.0.0.1:8765/hosts | grep -q "\"$NC_HOST_NAME\""'
if [ -n "$NC_DOMAIN" ]; then
  echo "https ($NC_DOMAIN)"
  check "DNS points $NC_DOMAIN here"      sh -c '[ "$(getent ahostsv4 "$NC_DOMAIN" | awk "NR==1{print \$1}")" = "$NC_PUBLIC_IP" ]'
  check "valid certificate (no -k)"       sh -c 'curl -sf -o /dev/null --max-time 20 "https://$NC_DOMAIN/"'
  check "reports secure mode"             sh -c 'curl -sf -X POST --max-time 10 "https://$NC_DOMAIN/auth" -d "{\"user\":\"nc\",\"password\":\"$NC_PASSWORD\"}" | grep -q "\"mode\":\"secure\""'
  check "local host listener (loopback)"  sh -c 'curl -sf -o /dev/null -u "nc:$NC_PASSWORD" http://127.0.0.1:8765/hosts'
  check "firewall allows 80,443/tcp"      sh -c 'ufw status | grep -q "^80/tcp" && ufw status | grep -q "^443/tcp"'
fi
echo
echo "$PASS ok, $FAIL failed"
[ "$FAIL" -eq 0 ]
