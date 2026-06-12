# Build a static Linux/amd64 binary for deployment to the VPS.
# Run from the project root: .\scripts\build.ps1

$env:GOOS       = "linux"
$env:GOARCH     = "amd64"
$env:CGO_ENABLED = "0"

go build -ldflags="-s -w" -o whatsapptool ./cmd/server/
$exitCode = $LASTEXITCODE

Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED

if ($exitCode -ne 0) {
    Write-Error "Build failed (exit $exitCode)"
    exit $exitCode
}

$size = (Get-Item "whatsapptool").Length / 1MB
Write-Host ("Built whatsapptool (linux/amd64, {0:N1} MB) — copy to VPS /opt/whatsapptool/" -f $size)
