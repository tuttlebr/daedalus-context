#!/bin/sh

url_ingest --input /files/nvidiadocs_url_links.txt --output /downloaded_urls/nvidia --input-type sitemap --max-age-days 365 --try-markdown --skip-existing --delay-ms 100
url_ingest --input /files/nvidia_url_links.txt --output /downloaded_urls/nvidia --input-type sitemap --max-age-days 365 --skip-existing --delay-ms 100
url_ingest --input /files/semianalysis_url_links.txt --output /downloaded_urls/semianalysis --input-type sitemap --max-age-days 365 --skip-existing --delay-ms 100
url_ingest --input /files/vetpartner_url_links.txt --output /downloaded_urls/vetpartner --input-type url --skip-existing --delay-ms 100
url_ingest --input /files/k8s_url_links.txt --output /downloaded_urls/kubernetes --input-type url --skip-existing --delay-ms 100
