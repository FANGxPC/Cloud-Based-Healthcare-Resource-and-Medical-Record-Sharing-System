# Sets ONE password of your choice for all 13 demo users in a deployed stack.
# The password is typed into a hidden prompt; it's never printed or committed.
#
#   powershell -ExecutionPolicy Bypass -File scripts\set-demo-password.ps1                # dev stack
#   powershell -ExecutionPolicy Bypass -File scripts\set-demo-password.ps1 -Stack hc-demo # demo stack
#
# Rules: at least 8 characters, with an upper-case letter, a lower-case letter and a digit.
param([string]$Stack = "hc-dev", [string]$Region = "us-east-1")
$ErrorActionPreference = "Stop"

$pool = aws cloudformation describe-stacks --stack-name $Stack --region $Region `
  --query "Stacks[0].Outputs[?OutputKey=='UserPoolId'].OutputValue" --output text
if (-not $pool) { throw "Could not find UserPoolId for stack $Stack" }

$secure = Read-Host "New demo password (hidden)" -AsSecureString
$pw = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
if ($pw.Length -lt 8 -or $pw -cnotmatch '[A-Z]' -or $pw -cnotmatch '[a-z]' -or $pw -notmatch '\d') {
  throw "Password needs 8+ characters with upper-case, lower-case and a digit."
}

$users = @()
foreach ($h in "h001", "h002", "h003", "h004", "h005") { $users += "doctor.$h@example.com", "admin.$h@example.com" }
$users += "patient1@example.com", "patient2@example.com", "patient3@example.com"

foreach ($u in $users) {
  aws cognito-idp admin-set-user-password --user-pool-id $pool --region $Region --username $u --password $pw --permanent | Out-Null
  Write-Host "  updated $u"
}

# Keep the local (git-ignored) copy in sync, used by scripts/e2e.
$envFile = Join-Path $PSScriptRoot "..\.env.demo-users"
Set-Content -Path $envFile -Encoding ascii -Value "# Demo login password for ALL seeded users (git-ignored, keep private)`nDEMO_PASSWORD=$pw"
Write-Host "Done. All $($users.Count) demo users now use your new password."
