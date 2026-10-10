# Builds the React frontend for a deployed stack and publishes it to S3 + CloudFront.
#
#   powershell -ExecutionPolicy Bypass -File scripts\deploy-frontend.ps1                 # dev
#   powershell -ExecutionPolicy Bypass -File scripts\deploy-frontend.ps1 -Stack hc-demo  # demo
#
# The API URL and Cognito IDs are read from the stack outputs, so nothing is hard-coded.
param([string]$Stack = "hc-dev", [string]$Region = "us-east-1")
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent

function Output($key) {
  aws cloudformation describe-stacks --stack-name $Stack --region $Region `
    --query "Stacks[0].Outputs[?OutputKey=='$key'].OutputValue" --output text
}
$bucket = Output "FrontendBucketName"
$distId = Output "FrontendDistributionId"
$site = Output "WebsiteUrl"

# 1. Build with this stack's settings (environment variables override .env files in Vite).
$env:VITE_API_URL = Output "ApiUrl"
$env:VITE_USER_POOL_ID = Output "UserPoolId"
$env:VITE_USER_POOL_CLIENT_ID = Output "UserPoolClientId"
Push-Location (Join-Path $root "frontend")
try {
  npm run build
  if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
} finally { Pop-Location }
$dist = Join-Path $root "frontend\dist"

# 2. Upload. Hashed assets are cached for a year; index.html is never cached, so new deploys appear at once.
aws s3 sync (Join-Path $dist "assets") "s3://$bucket/assets" --delete --region $Region `
  --cache-control "public,max-age=31536000,immutable"
aws s3 sync $dist "s3://$bucket" --delete --region $Region --exclude "assets/*" `
  --cache-control "no-cache"

# 3. Tell CloudFront to drop old copies (the first 1,000 invalidation paths each month are free).
aws cloudfront create-invalidation --distribution-id $distId --paths "/*" --query "Invalidation.Id" --output text | Out-Null

Write-Host ""
Write-Host "Frontend published: $site"
