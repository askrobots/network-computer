# Windows: remote automation, build box, and host

The dbbasic Flutter apps have macOS builds; Windows builds and testing need a
Windows box. There's a Windows 11 Pro VM available. This is how to set it up for
remote automation without anything exotic, the same way the Linux box works.

## 1. Remote automation = built-in OpenSSH Server

Windows 10/11 ship an OpenSSH server as an optional feature. Enabling it gives a
real remote shell (PowerShell over SSH, key-based), so it can be driven from here
exactly like the Linux box — no RDP, no third-party agent. In an elevated
PowerShell on the VM:

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server' -Enabled True `
  -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
# make PowerShell the default SSH shell
New-ItemProperty -Path 'HKLM:\SOFTWARE\OpenSSH' -Name DefaultShell `
  -Value 'C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe' -PropertyType String -Force
# authorize your key (admins use administrators_authorized_keys)
Add-Content "$env:ProgramData\ssh\administrators_authorized_keys" (Get-Content ~/.ssh/id_ed25519.pub)
icacls "$env:ProgramData\ssh\administrators_authorized_keys" /inheritance:r /grant "Administrators:F" /grant "SYSTEM:F"
```

After that: `ssh user@<vm-ip>` gets a PowerShell, and everything scriptable follows.

## 2. Provision it (winget)

winget is the package manager on Windows 11. A provisioning step, the Windows
analogue of `provision/apply.sh`, installs the toolchains:

```powershell
winget install --id Git.Git -e --accept-source-agreements
winget install --id Microsoft.VisualStudio.2022.BuildTools -e `
  --override "--quiet --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"
# Flutter
git clone --depth 1 -b stable https://github.com/flutter/flutter.git C:\flutter
$env:Path += ';C:\flutter\bin'
flutter config --enable-windows-desktop
# ffmpeg, for nc-host capture
winget install --id Gyan.FFmpeg -e
```

Visual Studio Build Tools with the "Desktop development with C++" workload is what
`flutter build windows` needs.

## 3. As a build box for the Flutter apps

Same idea as Linux, `flutter build windows --release` per app. The build sweep can
target Windows over SSH the same way it targets the Linux box. Windows build output
lands in `build\windows\x64\runner\Release\`.

## 4. As an nc-host (stream the Windows desktop)

`nc-host` is complete on Windows now: gdigrab capture, hardware H.264 where
available, and input injection via SendInput. Cross-compile and run it:

```sh
# from the Go repo, build the Windows binary
GOOS=windows GOARCH=amd64 go build -o nc-host.exe ./cmd/nc-host
# copy up and run (PowerShell), pointing at the rendezvous
scp nc-host.exe user@<vm-ip>:  ;  ssh user@<vm-ip> `
  '.\nc-host.exe -rendezvous https://<rendezvous> -name winbox -encoder h264_nvenc'
```

Then the Windows desktop is reachable and drivable from the phone, browser, or an
agent, just like the Linux one.

## Uploading builds to dbbasic.com

After a build, drop a `DBBASIC.md` in each app's directory with a `downloads:` block
listing the built artifacts per platform (see the format in this repo's
`../DBBASIC.md`). The dbbasic.com agent reads those and uploads/links the builds on
the site. The build sweep should generate or update each app's `DBBASIC.md` with the
artifact paths it just produced.

## Note

Provisioning here is documented as PowerShell rather than scripted into `provision/`
because it has not been run yet — treat it as a first setup. Once it works on the VM,
fold it into a `provision/windows.ps1` the same way Linux lives in `provision/`.
