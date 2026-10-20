#!/usr/bin/env python3
"""Read-only model, collection, and retrieval acceptance check."""
import json
import math
import os
from pathlib import Path

import requests
from pymilvus import MilvusClient
from refresh_state import contract_fingerprint


def main():
    contract = json.loads(Path('/etc/refresh/contract.json').read_text())
    database = contract.get('active_database', contract['milvus_database'])
    endpoint = os.environ['EMBEDDING_NIM_ENDPOINT'].rstrip('/') + '/embeddings'
    query = 'How do I configure this system?'
    def embed(payload):
        response = requests.post(endpoint, json={"model": contract['embedding']['model'], **payload}, timeout=120)
        response.raise_for_status()
        return response.json()['data'][0]['embedding']
    native = embed({'messages': [{'role': 'query', 'content': [{'type': 'text', 'text': query}]}]})
    nim = embed({'input': query, 'input_type': 'query'})
    assert len(native) == len(nim) == contract['embedding']['dimension']
    assert all(math.isfinite(x) for x in native)
    assert abs(sum(x*x for x in native) - 1) < .01
    assert sum(x*y for x,y in zip(native,nim)) > .999
    result = {'database': database, 'model': contract['embedding']['model'],
              'dimension': len(native), 'native_nim_query_parity': True, 'collections': {}}
    client = MilvusClient(uri=os.environ['MILVUS_URI'],
                          token=os.environ['MILVUS_USERNAME'] + ':' + os.environ['MILVUS_PASSWORD'],
                          db_name=database)
    try:
        for name in contract['collections']:
            desc = client.describe_collection(name)
            assert desc['description'] == 'daedalus-refresh:' + contract_fingerprint(contract)
            count = client.query(name, filter='', output_fields=['count(*)'], consistency_level='Strong')[0]['count(*)']
            assert count > 0, name + ' is empty'
            hits = client.search(name, data=[native], anns_field='vector', limit=3,
                                  output_fields=['text','source','content_metadata'],
                                  search_params={'metric_type':'COSINE','params':{'ef':64}}, consistency_level='Strong')
            assert hits[0] and hits[0][0]['entity']['text'].strip(), name + ' produced no searchable text'
            result['collections'][name] = {'records':count,'hits':len(hits[0]),'top_source':hits[0][0]['entity']['source']['source_id']}
    finally:
        client.close()
    print(json.dumps(result, sort_keys=True), flush=True)


if __name__ == '__main__':
    main()
