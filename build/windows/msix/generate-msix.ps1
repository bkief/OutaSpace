param(
  [Parameter(Mandatory=$true)]
  [string]$Version,
  [string]$ExePath = "build\bin\OutaSpace.exe",
  [string]$OutputDir = "dist",
  [string]$Publisher = "CN=Brian Kiefer"
)

$ErrorActionPreference = "Stop"

# Ensure version format X.Y.Z.0
$cleanVer = ($Version -replace '^v','').Trim()
$parts = $cleanVer.Split('.')
while ($parts.Count -lt 3) { $parts += "0" }
$appxVersion = "$($parts[0]).$($parts[1]).$($parts[2]).0"

Write-Host "Building MSIX for OutaSpace version $appxVersion..." -ForegroundColor Cyan

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$rootDir = (Get-Item $scriptDir).Parent.Parent.Parent.FullName
$appIconPath = Join-Path $rootDir "build\appicon.png"

$stagingDir = Join-Path $scriptDir "staging"
$assetsDir = Join-Path $stagingDir "Assets"

if (Test-Path $stagingDir) {
  Remove-Item -Path $stagingDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $assetsDir | Out-Null
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

# Copy binary
Copy-Item (Join-Path $rootDir $ExePath) (Join-Path $stagingDir "OutaSpace.exe") -Force

# Generate AppxManifest.xml
$manifestTemplate = Get-Content (Join-Path $scriptDir "AppxManifest.xml") -Raw
$manifestContent = $manifestTemplate -replace '__VERSION__', $appxVersion -replace '__PUBLISHER__', $Publisher
Set-Content -Path (Join-Path $stagingDir "AppxManifest.xml") -Value $manifestContent -NoNewline

# Helper to resize icons for MSIX assets
Add-Type -AssemblyName System.Drawing

function Resize-Icon {
  param([string]$Source, [string]$Dest, [int]$Width, [int]$Height)
  $srcImg = [System.Drawing.Image]::FromFile($Source)
  $destBitmap = New-Object System.Drawing.Bitmap($Width, $Height)
  $graphic = [System.Drawing.Graphics]::FromImage($destBitmap)
  $graphic.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $graphic.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
  $graphic.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
  $graphic.Clear([System.Drawing.Color]::Transparent)
  $graphic.DrawImage($srcImg, 0, 0, $Width, $Height)
  $destBitmap.Save($Dest, [System.Drawing.Imaging.ImageFormat]::Png)
  $graphic.Dispose()
  $destBitmap.Dispose()
  $srcImg.Dispose()
}

Write-Host "Generating MSIX visual assets..." -ForegroundColor Cyan
Resize-Icon $appIconPath (Join-Path $assetsDir "Square150x150Logo.png") 150 150
Resize-Icon $appIconPath (Join-Path $assetsDir "Square44x44Logo.png") 44 44
Resize-Icon $appIconPath (Join-Path $assetsDir "Square71x71Logo.png") 71 71
Resize-Icon $appIconPath (Join-Path $assetsDir "Square310x310Logo.png") 310 310
Resize-Icon $appIconPath (Join-Path $assetsDir "Wide310x150Logo.png") 310 150
Resize-Icon $appIconPath (Join-Path $assetsDir "StoreLogo.png") 50 50

# Locate MakeAppx.exe and signtool.exe
$makeAppx = Get-Command "MakeAppx.exe" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -First 1
if (-not $makeAppx) {
  $sdkPath = Get-ChildItem -Path "C:\Program Files*\Windows Kits\10\bin" -Filter "MakeAppx.exe" -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -like "*x64*" } | Select-Object -ExpandProperty FullName -First 1
  if ($sdkPath) { $makeAppx = $sdkPath }
}

if (-not $makeAppx) {
  throw "MakeAppx.exe could not be found. Please ensure Windows SDK is installed."
}

$msixOutput = Join-Path $OutputDir "OutaSpace-windows-amd64.msix"
Write-Host "Packaging MSIX with $makeAppx..." -ForegroundColor Cyan
& $makeAppx pack /d $stagingDir /p $msixOutput /o

# Locate signtool.exe and sign if available
$signTool = Get-Command "signtool.exe" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -First 1
if (-not $signTool) {
  $signPath = Get-ChildItem -Path "C:\Program Files*\Windows Kits\10\bin" -Filter "signtool.exe" -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -like "*x64*" } | Select-Object -ExpandProperty FullName -First 1
  if ($signPath) { $signTool = $signPath }
}

if ($signTool) {
  Write-Host "Creating self-signed certificate and signing MSIX..." -ForegroundColor Cyan
  $certSubject = $Publisher
  $pfxPath = Join-Path $scriptDir "temp_cert.pfx"
  $password = ConvertTo-SecureString -String "OutaSpace123!" -Force -AsPlainText
  
  $cert = New-SelfSignedCertificate -Type Custom -Subject $certSubject `
    -KeyUsage DigitalSignature -FriendlyName "OutaSpace Release" `
    -CertStoreLocation "Cert:\CurrentUser\My" `
    -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3")
  
  Export-PfxCertificate -Cert $cert -FilePath $pfxPath -Password $password | Out-Null
  
  & $signTool sign /fd SHA256 /a /f $pfxPath /p "OutaSpace123!" $msixOutput
  
  # Clean up certificate
  Remove-Item -Path $pfxPath -Force -ErrorAction SilentlyContinue
  Remove-Item -Path "Cert:\CurrentUser\My\$($cert.Thumbprint)" -Force -ErrorAction SilentlyContinue
  Write-Host "MSIX signed successfully!" -ForegroundColor Green
} else {
  Write-Warning "signtool.exe not found; MSIX packaged unsigned."
}

# Cleanup staging
Remove-Item -Path $stagingDir -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "MSIX package created at: $msixOutput" -ForegroundColor Green
