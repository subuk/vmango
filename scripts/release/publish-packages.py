#!/usr/bin/env python3
"""Submit source packages and wait for remote builds; never treat upload as success."""
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.parse
import urllib.request

PPA = 'https://api.launchpad.net/1.0/~subuk/+archive/ubuntu/vmango'


def get(url, **params):
    if params:
        url += '?' + urllib.parse.urlencode(params)
    with urllib.request.urlopen(url, timeout=60) as response:
        return json.load(response)


def entries(url, **params):
    page = get(url, **params)
    result = page['entries']
    while page.get('next_collection_link'):
        page = get(page['next_collection_link'])
        result.extend(page['entries'])
    return result


def sources(version, series):
    return entries(PPA, **{'ws.op': 'getPublishedSources', 'source_name': 'vmango',
                          'exact_match': 'true', 'version': version,
                          'distro_series': f'https://api.launchpad.net/1.0/ubuntu/{series}'})


def ppa_state(source, builds, binaries):
    if source['status'] not in ('Pending', 'Published'):
        raise RuntimeError('Source publication is ' + source['status'])
    pending = {'Needs building', 'Currently building', 'Uploading build', 'Cancelling'}
    for build in builds:
        state = build['buildstate']
        if state != 'Successfully built' and state not in pending:
            raise RuntimeError(f"Launchpad build {build['web_link']}: {state}")
    published_builds = {b['build_link'] for b in binaries
                        if b['status'] == 'Published' and b['binary_package_name'] == 'vmango'}
    return (source['status'] == 'Published' and bool(builds)
            and all(b['buildstate'] == 'Successfully built'
                    and b['self_link'] in published_builds for b in builds))


def ppa(version, series):
    existing = sources(version, series)
    if not existing:
        changes = list(pathlib.Path(f'dist/packages/ppa/{series}').glob('*_source.changes'))
        if len(changes) != 1:
            raise RuntimeError('Expected exactly one source changes file')
        subprocess.run(['bash', 'scripts/release/sign-ppa.sh', str(changes[0])], check=True)
        subprocess.run(['dput', 'ppa:subuk/vmango', str(changes[0])], check=True)
    else:
        print('Source version already exists; monitoring without uploading again.', flush=True)
    deadline = time.monotonic() + 4 * 3600
    previous = None
    while time.monotonic() < deadline:
        found = sources(version, series)
        status = 'Waiting for upload acceptance'
        if found:
            source = found[0]
            builds = entries(source['self_link'], **{'ws.op': 'getBuilds'})
            binaries = entries(source['self_link'], **{'ws.op': 'getPublishedBinaries'})
            status = [(b['web_link'], b['buildstate']) for b in builds]
            if ppa_state(source, builds, binaries):
                print(f'Published {version} for {series}: all builds succeeded.', flush=True)
                return
        if status != previous:
            print(status, flush=True)
            previous = status
        time.sleep(45)
    raise TimeoutError('PPA acceptance/build/publication timed out. Inspect Launchpad and rejection email; rerun to resume monitoring an accepted version.')


def copr(version):
    from copr.v3 import Client
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        config = pathlib.Path(tmp) / 'copr'
        config.write_text(os.environ['COPR_CONFIG'])
        config.chmod(0o600)
        client = Client.create_from_config_file(str(config))
        chroots = os.environ['COPR_CHROOTS'].split()
        if not chroots:
            raise RuntimeError('Set COPR_CHROOTS to the supported stable project chroots')
        project = client.project_proxy.get('subuk', 'vmango')
        if not set(chroots) <= set(project.chroot_repos):
            raise RuntimeError('Enable the requested chroots in stable COPR before publishing')
        # Reuse an accepted build of this release/revision on reruns, including failures.
        matches = []
        offset = 0
        while True:
            page = client.build_proxy.get_list('subuk', 'vmango', packagename='vmango',
                                               pagination={'limit': 100, 'offset': offset})
            matches.extend(b for b in page['items']
                           if (b.get('source_package') or {}).get('version') == version)
            if len(page['items']) < 100:
                break
            offset += 100
        if matches:
            build = max(matches, key=lambda b: b.id)
            if set(build.chroots) != set(chroots):
                raise RuntimeError('Existing build has different chroots; use a new package revision')
        else:
            srpms = list(pathlib.Path('dist/packages/rpm').glob('*.src.rpm'))
            if len(srpms) != 1:
                raise RuntimeError('Expected exactly one SRPM')
            build = client.build_proxy.create_from_file('subuk', 'vmango', str(srpms[0]),
                                                       buildopts={'chroots': chroots})
        print(f'https://copr.fedorainfracloud.org/coprs/build/{build.id}/', flush=True)
        deadline = time.monotonic() + 4 * 3600
        previous = None
        while time.monotonic() < deadline:
            build = client.build_proxy.get(build.id)
            if build.state != previous:
                print(f'COPR build {build.id}: {build.state}', flush=True)
                previous = build.state
            if build.state == 'succeeded':
                return
            if build.state in ('failed', 'canceled', 'skipped', 'unknown'):
                raise RuntimeError('COPR build did not succeed; inspect logs and resubmit with a new package revision if needed')
            time.sleep(45)
        raise TimeoutError('COPR build timed out; rerun to resume monitoring')


if __name__ == '__main__':
    if sys.argv[1] == 'copr':
        copr(f"{os.environ['VERSION']}-{os.environ['PACKAGE_REVISION']}.release")
    elif sys.argv[1] == 'ppa':
        ppa(f"{os.environ['VERSION']}-{os.environ['PACKAGE_REVISION']}ppa1~{os.environ['SERIES']}", os.environ['SERIES'])
    else:
        raise SystemExit('Expected copr or ppa')
