# DNS Sync

I like an idea of infrastructure as a code.

This is a Cloudflare DNS as a code solution. Store DNS records in yaml file, keep tracking what's change in git and let CI/CD sync it for you

- DNS Token can be create at [Cloudflare API Token](https://dash.cloudflare.com/profile/api-tokens) (My profile > API Tokens > Create Token)
- DNS Zone ID can be found at Websites > Overview > API (Right side) > Zone ID

## Usage

### Github Action

```yaml
name: Sync DNS

on:
  push:
    branches: ['main']

jobs:
  sync-dns:
    runs-on: ubuntu-latest
    permissions:
      contents: read

    steps:
      - name: Checkout repository
        uses: actions/checkout@v2

      - name: Sync DNS
        uses: nitpum/dns-sync@main
        with:
          cloudflare-token: ${{ secrets.CLOUDFLARE_TOKEN }}
          dns-zone-id: ${{ secrets.DNS_ZONE_ID }}
```

### Binary

```bash
dns-sync <DNS_TOKEN> <DNS_ZONE_ID> config.yaml
```
