import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class VersionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'scripts').mkdir()
        shutil.copy(ROOT / 'scripts/version.sh', self.root / 'scripts/version.sh')
        shutil.copy(ROOT / 'Makefile', self.root / 'Makefile')
        for name in ('templates', 'static'):
            (self.root / name).mkdir()
        self.env = dict(os.environ)
        self.env.pop('VERSION', None)
        self.run_command('git', 'init', '-q')
        self.run_command('git', 'config', 'user.email', 'test@example.com')
        self.run_command('git', 'config', 'user.name', 'Version test')
        self.run_command('git', 'add', '.')
        self.run_command('git', 'commit', '-qm', 'Initial source')

    def run_command(self, *args, cwd=None):
        return subprocess.check_output(args, cwd=cwd or self.root, env=self.env, text=True).strip()

    def version(self, *args):
        return self.run_command('make', '-s', 'print-version', *args)

    def test_tag_and_subsequent_commit(self):
        self.run_command('git', 'tag', 'v1.2.3')
        self.assertEqual(self.version(), '1.2.3')
        self.run_command('git', 'commit', '--allow-empty', '-qm', 'Next commit')
        self.assertRegex(self.version(), r'^1\.2\.3\.1\.g[0-9a-f]+$')
        self.assertEqual(self.run_command('sh', 'scripts/version.sh', '--base'), '1.2.3')

    def test_prerelease(self):
        self.run_command('git', 'tag', 'v1.2.3-rc.1')
        self.assertEqual(self.version(), '1.2.3-rc.1')
        self.assertEqual(self.run_command('sh', 'scripts/version.sh', '--base'), '0.0.0')

    def test_no_tags(self):
        self.assertRegex(self.version(), r'^0\.0\.0\.g[0-9a-f]+$')
        self.assertEqual(self.run_command('sh', 'scripts/version.sh', '--base'), '0.0.0')

    def test_overrides_and_archive_metadata(self):
        (self.root / 'VERSION').write_text('2.3.4\n')
        self.assertEqual(self.version(), '2.3.4')
        self.env['VERSION'] = '3.4.5'
        self.assertEqual(self.version(), '3.4.5')
        self.assertEqual(self.version('VERSION=4.5.6'), '4.5.6')

    def test_without_git(self):
        shutil.rmtree(self.root / '.git')
        self.assertEqual(self.version(), '0.0.0-unknown')
        (self.root / 'VERSION').write_text('1.2.3\n')
        self.assertEqual(self.version(), '1.2.3')

    def test_package_source_archive_preserves_requested_version(self):
        self.check_package_archive('6.7.8')

    def test_package_source_archive_preserves_test_version(self):
        self.check_package_archive('6.7.8~test.123.g' + 'a' * 40)

    def check_package_archive(self, version):
        shutil.copy(ROOT / 'scripts/release/package-sources.sh', self.root / 'package-sources.sh')
        (self.root / 'vmango.spec.in').write_text('Version: @@_VERSION_@@\n')
        self.run_command('git', 'add', '.')
        self.run_command('git', 'commit', '-qm', 'Packaging fixture')
        # Stop at the container boundary after exercising the real archive creation.
        fake_bin = self.root / 'fake-bin'
        fake_bin.mkdir()
        docker = fake_bin / 'docker'
        docker.write_text('#!/bin/sh\nexit 42\n')
        docker.chmod(0o755)
        self.env.update(VERSION=version, PACKAGE_REVISION='1',
                        PATH=str(fake_bin) + os.pathsep + self.env['PATH'])
        result = subprocess.run(['bash', 'package-sources.sh'], cwd=self.root, env=self.env)
        self.assertEqual(result.returncode, 42)
        value = self.run_command('tar', '-xOf', f'dist/packages/vmango-{version}.tar.gz',
                                 f'vmango-{version}/VERSION')
        self.assertEqual(value, version)

    def test_tarball_preserves_override_without_git(self):
        tar = shutil.which('gtar') or shutil.which('tar')
        self.run_command('make', '-s', 'tarball', 'VERSION=5.6.7', f'TAR={tar}',
                         'TARBALL_SOURCES=Makefile scripts/version.sh templates static')
        destination = self.root / 'unpacked'
        destination.mkdir()
        self.run_command(tar, '-xzf', str(self.root / 'vmango-5.6.7.tar.gz'),
                         '-C', str(destination))
        self.assertEqual(self.run_command('make', '-s', 'print-version',
                                         cwd=destination / 'vmango-5.6.7'), '5.6.7')


if __name__ == '__main__':
    unittest.main()
