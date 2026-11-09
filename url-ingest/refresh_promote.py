"""Promote a verified model generation while retaining the previous database.

Stop refresh writers first. This is resumable: every move is recorded on the
manifest PVC, and matching collections already promoted are left in place.
"""
import contextlib
import json
import os
import time
from pathlib import Path
from pymilvus import MilvusClient
from refresh_state import atomic_json, collection_lock, contract_fingerprint, refresh_owned

contract = json.loads(Path('/etc/refresh/contract.json').read_text())
source_db = contract['milvus_database']
archive_db = 'legacy_pre_nemotron35_20261020'
expected = 'daedalus-refresh:' + contract_fingerprint(contract)
root = Path('/manifests/refresh') / contract['generation']
record = root / 'promotion.json'
client = MilvusClient(uri=os.environ['MILVUS_URI'], token=os.environ['MILVUS_USERNAME']+':'+os.environ['MILVUS_PASSWORD'])
state = json.loads(record.read_text()) if record.exists() else {'source':source_db,'target':'default','archive':archive_db,'moves':[]}
with contextlib.ExitStack() as stack:
    for name in contract['collections']:
        stack.enter_context(collection_lock(root/'locks',name))
    # Validate the complete staged generation before moving any existing data.
    client.using_database(source_db)
    staged = set(client.list_collections())
    for name in contract['collections']:
        # A resumed promotion can have moved some collections already. Every
        # required collection must exist in one location and match the contract.
        client.using_database(source_db if name in staged else 'default')
        if client.describe_collection(name)['description'] != expected:
            raise ValueError(f'{name} does not match the new retrieval contract')
        count = client.query(name, filter='', output_fields=['count(*)'],consistency_level='Strong')[0]['count(*)']
        if count <= 0:
            raise ValueError(f'{name} is empty; refusing promotion')
    if archive_db not in client.list_databases():
        client.create_database(archive_db)
    client.using_database('default')
    for name in list(client.list_collections()):
        description = client.describe_collection(name).get('description')
        if description == expected:
            continue
        # The Agent's `meta` and private upload collections share this database.
        if not refresh_owned(name, description, contract['collections']):
            print('Retained', name, flush=True)
            continue
        client.rename_collection(name, name, target_db=archive_db, timeout=60)
        state['moves'].append({'from':'default','to':archive_db,'collection':name,'at':time.time()})
        atomic_json(record,state)
        print('Archived',name,flush=True)
    client.using_database(source_db)
    for name in contract['collections']:
        if name in client.list_collections():
            client.rename_collection(name,name,target_db='default',timeout=60)
            state['moves'].append({'from':source_db,'to':'default','collection':name,'at':time.time()})
            atomic_json(record,state)
            print('Promoted',name,flush=True)
    client.using_database('default')
    for name in contract['collections']:
        assert client.describe_collection(name)['description'] == expected
        assert client.query(name,filter='',output_fields=['count(*)'],consistency_level='Strong')[0]['count(*)'] > 0
    state['complete']=True
    atomic_json(record,state)
client.close()
print('Promotion verified; archived vectors remain available in',archive_db,flush=True)
