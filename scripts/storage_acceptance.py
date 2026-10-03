"""Exercise JSONL migration and SQLite backup/restore through the actual CLI."""
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile

binary = str(pathlib.Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix='elephant-storage-') as tmp:
    root = pathlib.Path(tmp)

    def cli(command, store, *extra, body=None, success=True):
        p = subprocess.run([binary, command, '--store', str(store), '--unattached', *extra],
                           input=json.dumps(body) if body is not None else None,
                           capture_output=True, text=True)
        if not success:
            assert p.returncode != 0, p.stdout
            return
        assert p.returncode == 0, p.stderr
        return json.loads(p.stdout) if p.stdout.strip() else None

    memory = cli('record', root / 'seed.sqlite', body={
        'class': 'lesson', 'incident': 'SYNTHETIC migration evidence',
        'lesson': 'Check backups before restore', 'source': 'SYNTHETIC storage acceptance'})
    legacy = root / 'legacy with spaces.jsonl'
    feedback = {'kind': 'feedback', 'id': memory['id'], 'feedback_id': 'me:legacy-run', 'helpful': True}
    original = ''.join(json.dumps(e) + '\n' for e in [
        {'kind': 'put', 'memory': memory}, feedback, feedback]).encode()
    legacy.write_bytes(original)
    migrated = cli('list', legacy)['memories']
    assert len(migrated) == 1 and migrated[0]['id'] == memory['id'] and migrated[0]['helpful'] == 1
    assert legacy.read_bytes().startswith(b'SQLite format 3\0')
    assert pathlib.Path(str(legacy) + '.jsonl-backup').read_bytes() == original
    cli('feedback', legacy, '--id', memory['id'], '--feedback-id', 'legacy-run')
    assert cli('list', legacy)['memories'][0]['helpful'] == 1  # Old dedup keys survive.
    backup = root / 'new backup.sqlite'
    restored = root / 'new restored.sqlite'
    cli('backup', legacy, '--output', str(backup))
    checksum = hashlib.sha256(backup.read_bytes()).digest()
    cli('backup', legacy, '--output', str(backup), success=False)
    cli('restore', legacy, '--file', str(backup), '--output', str(restored))
    assert hashlib.sha256(backup.read_bytes()).digest() == checksum
    cli('restore', legacy, '--file', str(backup), '--output', str(restored), success=False)
    assert cli('list', restored)['memories'] == migrated
    assert cli('doctor', restored)['ok']
    corrupt = root / 'corrupt.jsonl'
    corrupt.write_bytes(original + b'{partial')
    cli('list', corrupt, success=False)
    assert corrupt.read_bytes() == original + b'{partial'
print('CLI migration, dedup, original backup, SQLite backup/restore, integrity and fail-closed checks passed.')
