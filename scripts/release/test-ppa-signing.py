#!/usr/bin/env python3
"""Exercise debsign and dput with a disposable key and no network upload."""
import os
import pathlib
import shutil
import subprocess
import tempfile

with tempfile.TemporaryDirectory() as temp:
    root = pathlib.Path(temp)
    home = root / 'gnupg'
    home.mkdir(mode=0o700)
    env = dict(os.environ, GNUPGHOME=str(home))
    try:
        subprocess.run(['gpg', '--batch', '--pinentry-mode', 'loopback', '--passphrase', 'ci-test-only',
                        '--quick-generate-key', 'Vmango CI test <ci@example.invalid>',
                        'rsa2048', 'sign', '1d'], env=env, check=True, capture_output=True)
        listing = subprocess.check_output(['gpg', '--with-colons', '--list-secret-keys'], env=env, text=True)
        fingerprint = next(line.split(':')[9] for line in listing.splitlines() if line.startswith('fpr:'))
        key = subprocess.check_output(['gpg', '--batch', '--pinentry-mode', 'loopback',
                                       '--passphrase', 'ci-test-only', '--armor',
                                       '--export-secret-keys', fingerprint], env=env, text=True)
        package = root / 'package'
        shutil.copytree('dist/packages/ppa/jammy', package)
        changes = next(package.glob('*_source.changes'))
        # Do not expose the generation keyring to the signing/upload helper.
        signing_env = dict(os.environ, PPA_GPG_PRIVATE_KEY=key,
                           PPA_GPG_PASSPHRASE='ci-test-only', PPA_GPG_FINGERPRINT=fingerprint)
        signing_env['PUBLICATION_CHANNEL'] = ('devel' if '~test.' in signing_env['VERSION'] else 'stable')
        signing_env['SERIES'] = 'jammy'
        subprocess.run(['bash', 'scripts/release/sign-ppa.sh', str(changes), '--simulate'],
                       env=signing_env, check=True)
        for file in [changes, *package.glob('*.dsc'), *package.glob('*.buildinfo')]:
            subprocess.run(['gpg', '--verify', str(file)], env=env, check=True)
    finally:
        subprocess.run(['gpgconf', '--kill', 'gpg-agent'], env=env, check=False)
