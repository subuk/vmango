#!/usr/bin/env python3
"""Submit source packages and wait for remote builds; never treat upload as success."""
import json
from email.parser import Parser
import os
import pathlib
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request

CHROOTS = ['rhel-8-x86_64', 'rhel-9-x86_64', 'rhel-10-x86_64']
SERIES = ('jammy', 'noble', 'resolute')


def destination(channel, version):
    """Keep untagged test versions out of the stable repositories."""
    patterns = {'stable': r'[0-9]+\.[0-9]+\.[0-9]+',
                'devel': r'[0-9]+\.[0-9]+\.[0-9]+~test\.[1-9][0-9]*\.g[0-9a-f]{40}'}
    if channel not in patterns or not re.fullmatch(patterns[channel], version):
        raise ValueError('Version does not match the requested publication channel')
    return 'vmango' if channel == 'stable' else 'vmango-devel'


def ppa_url(project):
    return f'https://api.launchpad.net/devel/~subuk/+archive/ubuntu/{project}'


def validate_changes(path, version, revision, series):
    """Verify the actual source upload, not just the workflow inputs."""
    if series not in SERIES or not re.fullmatch(r'[1-9][0-9]*', revision):
        raise ValueError('Unsupported source package revision or series')
    fields = Parser().parsestr(pathlib.Path(path).read_text())
    expected = f'{version}-{revision}ppa1~{series}'
    if (fields.get('Source') != 'vmango' or fields.get('Version') != expected
            or fields.get('Distribution') != series):
        raise ValueError('Source changes do not match the requested package version and series')


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


def sources(version, series, project):
    return entries(ppa_url(project), **{'ws.op': 'getPublishedSources', 'source_name': 'vmango',
                          'exact_match': 'true', 'version': version,
                          'distro_series': f'https://api.launchpad.net/devel/ubuntu/{series}'})


def ppa_state(source, builds, binaries):
    if source['status'] not in ('Pending', 'Published'):
        raise RuntimeError('Source publication is ' + source['status'])
    pending = {'Needs building', 'Currently building', 'Uploading build', 'Cancelling'}
    for build in builds:
        if build.get('arch_tag') != 'amd64':
            raise RuntimeError('Expected only amd64 PPA builds')
        state = build['buildstate']
        if state != 'Successfully built' and state not in pending:
            raise RuntimeError(f"Launchpad build {build['web_link']}: {state}")
    published_builds = {b['build_link'] for b in binaries
                        if b['status'] == 'Published' and b['binary_package_name'] == 'vmango'}
    return (source['status'] == 'Published' and bool(builds)
            and all(b['buildstate'] == 'Successfully built'
                    and b['self_link'] in published_builds for b in builds))


def ppa(version, series, project):
    if series not in SERIES:
        raise ValueError('Unsupported Ubuntu series')
    archive = get(ppa_url(project))
    processors = entries(archive['processors_collection_link'])
    if {p['name'] for p in processors} != {'amd64'}:
        raise RuntimeError('Configure the PPA to build only on amd64')
    existing = sources(version, series, project)
    if not existing:
        changes = list(pathlib.Path(f'dist/packages/ppa/{series}').glob('*_source.changes'))
        if len(changes) != 1:
            raise RuntimeError('Expected exactly one source changes file')
        subprocess.run(['bash', 'scripts/release/sign-ppa.sh', str(changes[0])], check=True)
    else:
        print('Source version already exists; monitoring without uploading again.', flush=True)
    deadline = time.monotonic() + 4 * 3600
    previous = None
    while time.monotonic() < deadline:
        found = sources(version, series, project)
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


def copr_builds(client, version, project='vmango'):
    matches = []
    offset = 0
    while True:
        # copr.v3 unwraps the API's JSON envelope into a list with metadata.
        page = client.build_proxy.get_list('subuk', project, packagename='vmango',
                                          pagination={'limit': 100, 'offset': offset})
        # COPR may report Version-Release without the SRPM's dist suffix.
        versions = {version, version.removesuffix('.release')}
        matches.extend(b for b in page
                       if (b.get('source_package') or {}).get('version') in versions)
        if len(page) < 100:
            return matches
        offset += 100


def copr(version, project_name):
    from copr.v3 import Client
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        config = pathlib.Path(tmp) / 'copr'
        config.write_text(os.environ['COPR_CONFIG'])
        config.chmod(0o600)
        client = Client.create_from_config_file(str(config))
        chroots = CHROOTS
        project = client.project_proxy.get('subuk', project_name)
        if not set(chroots) <= set(project.chroot_repos):
            raise RuntimeError('Enable the requested chroots in COPR before publishing')
        # Reuse an accepted build of this release/revision on reruns, including failures.
        matches = copr_builds(client, version, project_name)
        if matches:
            build = max(matches, key=lambda b: b.id)
            if set(build.chroots) != set(chroots):
                raise RuntimeError('Existing build has different chroots; use a new package revision')
        else:
            srpms = list(pathlib.Path('dist/packages/rpm').glob('*.src.rpm'))
            if len(srpms) != 1:
                raise RuntimeError('Expected exactly one SRPM')
            build = client.build_proxy.create_from_file('subuk', project_name, str(srpms[0]),
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
    project = destination(os.environ['PUBLICATION_CHANNEL'], os.environ['VERSION'])
    if not re.fullmatch(r'[1-9][0-9]*', os.environ['PACKAGE_REVISION']):
        raise SystemExit('Expected a positive package revision')
    if sys.argv[1] == 'copr':
        copr(f"{os.environ['VERSION']}-{os.environ['PACKAGE_REVISION']}.release", project)
    elif sys.argv[1] == 'ppa':
        ppa(f"{os.environ['VERSION']}-{os.environ['PACKAGE_REVISION']}ppa1~{os.environ['SERIES']}", os.environ['SERIES'], project)
    else:
        raise SystemExit('Expected copr or ppa')
