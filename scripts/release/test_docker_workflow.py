"""Exercise Docker upload decisions with local Docker and GitHub CLI substitutes."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[2]


class DockerPublicationTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        workflow = (ROOT / '.github/workflows/publish-docker.yml').read_text()
        self.workflow = workflow
        step = workflow.split('      - name: Push release image and update latest\n', 1)[1]
        self.script = textwrap.dedent(step.split('        run: |\n', 1)[1])
        program = '#!' + sys.executable + '\n' + textwrap.dedent('''\
            import json
            import os
            from pathlib import Path
            import sys
            tool = Path(sys.argv[0]).name
            args = sys.argv[1:]
            with Path('calls.jsonl').open('a') as log:
                log.write(json.dumps([tool, *args]) + '\\n')
            if tool == 'gh':
                if os.environ.get('FAIL_API'):
                    sys.exit(1)
                assert args[0] == 'api' and args[1].endswith('/releases/latest'), args
                print(os.environ['LATEST_TAG'])
            elif args[0] == 'inspect':
                label = 'IMAGE_VERSION' if 'image.version' in args[2] else 'IMAGE_SHA'
                print(os.environ[label])
            elif args[0] == 'push' and os.environ.get('FAIL_PUSH'):
                sys.exit(1)
            else:
                assert args[0] in ('load', 'tag', 'push'), args
            ''')
        for name in ('docker', 'gh'):
            path = self.root / name
            path.write_text(program)
            path.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ['PATH'],
                        VERSION='0.15.0', LATEST_TAG='v0.15.0', GH_REPO='example/vmango',
                        SOURCE_SHA='a' * 40, IMAGE_SHA='a' * 40, IMAGE_VERSION='0.15.0',
                        RUNNER_TEMP=str(self.root), FAIL_PUSH='', FAIL_API='')

    def run_script(self, script=None):
        result = subprocess.run(['bash', '-eu', '-o', 'pipefail', '-c', script or self.script],
                                cwd=self.root, env=self.env, capture_output=True, text=True)
        log = self.root / 'calls.jsonl'
        calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
        return result, calls

    def pushes(self, calls):
        return [call[2] for call in calls if call[:2] == ['docker', 'push']]

    def test_latest_stable_publishes_both_tags(self):
        result, calls = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.pushes(calls), ['subuk/vmango:0.15.0', 'subuk/vmango:latest'])

    def test_older_release_does_not_roll_latest_back(self):
        self.env['LATEST_TAG'] = 'v0.16.0'
        result, calls = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.pushes(calls), ['subuk/vmango:0.15.0'])

    def test_prerelease_publishes_only_version_without_latest_lookup(self):
        self.env.update(VERSION='0.16.0-rc.1', FAIL_API='1')
        result, calls = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.pushes(calls), ['subuk/vmango:0.16.0-rc.1'])
        self.assertFalse(any(call[0] == 'gh' for call in calls))

    def test_failed_version_push_stops_before_latest(self):
        self.env['FAIL_PUSH'] = '1'
        result, calls = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.pushes(calls), ['subuk/vmango:0.15.0'])
        self.assertFalse(any(call[0] == 'gh' for call in calls))

    def test_api_failure_never_updates_latest(self):
        self.env['FAIL_API'] = '1'
        result, calls = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.pushes(calls), ['subuk/vmango:0.15.0'])

    def test_wrong_artifact_version_or_revision_stops_before_login(self):
        step = self.workflow.split('      - name: Load and verify tested image\n', 1)[1]
        script = textwrap.dedent(step.split('        run: |\n', 1)[1].split('      - uses:', 1)[0])
        for override in ({}, {'IMAGE_VERSION': '0.14.0'}, {'IMAGE_SHA': 'b' * 40}):
            with self.subTest(override=override):
                self.env.update({'IMAGE_VERSION': '0.15.0', 'IMAGE_SHA': 'a' * 40, **override})
                result, _ = self.run_script(script)
                self.assertEqual(result.returncode == 0, not override, result.stderr)


if __name__ == '__main__':
    unittest.main()
