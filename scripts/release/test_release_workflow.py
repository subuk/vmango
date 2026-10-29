"""Exercise the actual publication shell with a local GitHub CLI substitute."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ReleaseWorkflowTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        step = workflow.split('      - name: Create checksums and publish complete release\n', 1)[1]
        self.script = textwrap.dedent(step.split('        run: |\n', 1)[1].split('\n  packages:', 1)[0])
        for suffix in ('linux-x64.tar.gz', 'windows-x64.zip', 'macos-arm64.tar.gz'):
            (self.root / ('vmango-0.12.2-' + suffix)).write_bytes(b'test artifact')
        gh = self.root / 'gh'
        gh.write_text('#!' + sys.executable + '\n' + textwrap.dedent('''\
            import json
            import os
            from pathlib import Path
            import subprocess
            import sys
            args = sys.argv[1:]
            with Path('calls.jsonl').open('a') as log:
                log.write(json.dumps(args) + '\\n')
            if args[0] == 'api':
                endpoint = args[1]
                if os.environ.get('FAIL_ENDPOINT', '') in endpoint and os.environ.get('FAIL_ENDPOINT'):
                    print('{"message":"API unavailable"}')
                    print('gh: API unavailable (HTTP 503)', file=sys.stderr)
                    sys.exit(1)
                if '/git/matching-refs/' in endpoint:
                    data = [{'ref': ref} for ref in json.loads(os.environ['TAG_REFS'])]
                elif '/commits/' in endpoint:
                    if not json.loads(os.environ['TAG_REFS']):
                        print('{"message":"No commit found for SHA", "status":"422"}')
                        print('gh: No commit found for SHA (HTTP 422)', file=sys.stderr)
                        sys.exit(1)
                    data = {'sha': os.environ['TAG_SHA']}
                else:
                    raise AssertionError(endpoint)
                result = subprocess.run(['jq', '-r', args[args.index('--jq') + 1]],
                                        input=json.dumps(data), text=True)
                sys.exit(result.returncode)
            assert args[:2] in (['release', 'create'], ['release', 'edit']), args
            '''))
        gh.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ['PATH'],
                        VERSION='0.12.2', SOURCE_SHA='a' * 40, TAG_SHA='a' * 40,
                        TAG_REFS='[]', GH_REPO='example/vmango', GH_TOKEN='test-token',
                        FAIL_ENDPOINT='')

    def run_publication(self):
        result = subprocess.run(['bash', '-e', '-c', self.script], cwd=self.root,
                                env=self.env, capture_output=True, text=True)
        log = self.root / 'calls.jsonl'
        calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
        return result, calls

    def test_new_tag_publishes(self):
        result, calls = self.run_publication()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([call[:2] for call in calls[-2:]],
                         [['release', 'create'], ['release', 'edit']])
        self.assertEqual(len((self.root / 'SHA256SUMS').read_text().splitlines()), 3)

    def test_prefix_match_is_not_an_existing_release_tag(self):
        self.env['TAG_REFS'] = '["refs/tags/v0.12.20", "refs/tags/v0.12.2-rc.1"]'
        result, calls = self.run_publication()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any('/commits/' in call[1] for call in calls))

    def test_matching_tag_publishes(self):
        self.env['TAG_REFS'] = '["refs/tags/v0.12.2"]'
        result, calls = self.run_publication()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(any('/commits/v0.12.2' in call[1] for call in calls))
        self.assertEqual(calls[-1][:2], ['release', 'edit'])

    def test_conflicting_tag_stops_before_publication(self):
        self.env.update(TAG_REFS='["refs/tags/v0.12.2"]', TAG_SHA='b' * 40)
        result, calls = self.run_publication()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('expected ' + self.env['SOURCE_SHA'], result.stderr)
        self.assertFalse(any(call[0] == 'release' for call in calls))

    def test_api_errors_stop_before_publication(self):
        for endpoint in ('/git/matching-refs/', '/commits/'):
            with self.subTest(endpoint=endpoint):
                (self.root / 'calls.jsonl').write_text('')
                self.env.update(FAIL_ENDPOINT=endpoint, TAG_REFS='["refs/tags/v0.12.2"]')
                result, calls = self.run_publication()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('API unavailable', result.stderr)
                self.assertFalse(any(call[0] == 'release' for call in calls))

    def test_missing_asset_stops_before_publication(self):
        (self.root / 'vmango-0.12.2-windows-x64.zip').unlink()
        result, calls = self.run_publication()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])


if __name__ == '__main__':
    unittest.main()
