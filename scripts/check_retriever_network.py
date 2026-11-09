#!/usr/bin/env python3
"""Verify the Cilium prerequisite before deploying SNAT forwarding rules."""
import json
import os
import subprocess
import sys

import yaml


def needs_source_filter(resources):
    return any(
        resource.get('kind') == 'CiliumNetworkPolicy'
        and any('world' in rule.get('fromEntities', [])
                for rule in resource.get('spec', {}).get('ingress', []))
        for resource in resources if resource
    )


def main():
    resources = list(yaml.safe_load_all(sys.stdin))
    if needs_source_filter(resources):
        result = subprocess.run(
            [os.environ.get('KUBECTL', 'kubectl'), '-n', 'kube-system', 'get',
             'configmap', 'cilium-config', '-o', 'json'],
            check=True, capture_output=True, text=True,
        )
        if json.loads(result.stdout).get('data', {}).get('bpf-lb-source-range-all-types') != 'true':
            raise SystemExit('Refusing retrieval forwarding: Cilium bpf-lb-source-range-all-types must be true.')
    if any(r and r.get('kind') == 'Service' and
           r.get('spec', {}).get('loadBalancerClass') == 'daedalus.ai/nodeport-only'
           for r in resources):
        # Helm 3 waits indefinitely for ingress addresses on this custom class.
        # The caller must instead verify Deployment rollout and client paths.
        print('nodeport-only')


if __name__ == '__main__':
    main()
