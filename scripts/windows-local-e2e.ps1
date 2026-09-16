[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$HostName,
    [Parameter(Mandatory = $true)][string]$Image,
    [Parameter(Mandatory = $true)][System.Security.SecureString]$Token,
    [Parameter(Mandatory = $true)][string]$Repository,
    [Parameter(Mandatory = $true)][int]$PullRequest,
    [string]$Gateway = ".\gh-gateway.exe",
    [switch]$NoSshProxy
)

$ErrorActionPreference = "Stop"
$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "This command must be run from an elevated terminal."
}

$hostsPath = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
$started = $false
$previousGhHost = $env:GH_HOST
$previousGhToken = $env:GH_ENTERPRISE_TOKEN
$previousPromptDisabled = $env:GH_PROMPT_DISABLED
try {
    $arguments = @("start", $HostName, "--image", $Image)
    if ($NoSshProxy) { $arguments += "--no-ssh-proxy" }
    & $Gateway @arguments
    if ($LASTEXITCODE -ne 0) { throw "gh-gateway start failed" }
    $started = $true

    $hostsText = Get-Content -LiteralPath $hostsPath -Raw
    if ($hostsText -notmatch "(?m)^127\.0\.0\.1\s+$([regex]::Escape($HostName))\s*$") { throw "hosts override was not installed" }

    $response = Invoke-WebRequest -Uri "https://$HostName/" -UseBasicParsing
    if ($response.StatusCode -ge 500) { throw "ordinary Gitea passthrough returned HTTP $($response.StatusCode)" }

    $env:GH_HOST = $HostName
    $env:GH_ENTERPRISE_TOKEN = [System.Net.NetworkCredential]::new("", $Token).Password
    $env:GH_PROMPT_DISABLED = "1"
    gh api --hostname $HostName user | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "gh api user failed" }
    gh repo view "$HostName/$Repository" --json nameWithOwner,parent | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "gh repo view failed" }
    gh pr view $PullRequest --repo "$HostName/$Repository" --json number,url,state | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "gh pr view failed" }

    $status = & $Gateway status
    $statusText = $status -join "`n"
    if ($LASTEXITCODE -ne 0 -or $statusText -notmatch "Status: running") { throw "status did not report running: $statusText" }
}
finally {
    if ($started) {
        & $Gateway stop
        if ($LASTEXITCODE -ne 0) { Write-Error "gh-gateway stop failed during cleanup" }
    }
    $remainingHosts = Get-Content -LiteralPath $hostsPath -Raw
    if ($remainingHosts -match "(?m)^127\.0\.0\.1\s+$([regex]::Escape($HostName))\s*$") { Write-Error "gh-gateway hosts mapping remained after cleanup" }
    $containers = docker ps -a --filter "label=io.gh-gateway.host=$HostName" --format "{{.ID}}"
    if ($containers) { Write-Error "gh-gateway container remained after cleanup: $containers" }
    $env:GH_HOST = $previousGhHost
    $env:GH_ENTERPRISE_TOKEN = $previousGhToken
    $env:GH_PROMPT_DISABLED = $previousPromptDisabled
}

Write-Host "Windows local transparent mode E2E passed."
