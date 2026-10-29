import importlib.util
import pathlib
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('publish_packages', pathlib.Path(__file__).with_name('publish-packages.py'))
publication = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publication)


class PublicationStateTest(unittest.TestCase):
    def setUp(self):
        self.source = {'status': 'Published'}
        self.build = {'self_link': 'build/1', 'web_link': 'https://example.com/build/1',
                      'arch_tag': 'amd64',
                      'buildstate': 'Successfully built'}
        self.binary = {'build_link': 'build/1', 'binary_package_name': 'vmango', 'status': 'Published'}

    def test_uploaded_source_without_builds_is_not_success(self):
        self.assertFalse(publication.ppa_state(self.source, [], []))

    def test_all_architectures_must_finish(self):
        pending = dict(self.build, self_link='build/2', buildstate='Needs building')
        self.assertFalse(publication.ppa_state(self.source, [self.build, pending], [self.binary]))

    def test_successful_build_must_be_published(self):
        self.assertFalse(publication.ppa_state(self.source, [self.build], [dict(self.binary, status='Pending')]))
        self.assertTrue(publication.ppa_state(self.source, [self.build], [self.binary]))

    def test_unrelated_binary_does_not_complete_build(self):
        self.assertFalse(publication.ppa_state(self.source, [self.build], [dict(self.binary, build_link='build/2')]))

    def test_failed_build_fails_immediately(self):
        with self.assertRaises(RuntimeError):
            publication.ppa_state(self.source, [dict(self.build, buildstate='Failed to build')], [])

    def test_removed_source_is_not_success(self):
        with self.assertRaises(RuntimeError):
            publication.ppa_state({'status': 'Deleted'}, [self.build], [self.binary])

    def test_non_amd64_build_is_rejected(self):
        build = dict(self.build, arch_tag='arm64')
        with self.assertRaisesRegex(RuntimeError, 'amd64'):
            publication.ppa_state(self.source, [build], [self.binary])


class DestinationTest(unittest.TestCase):
    test_version = '0.13.0~test.123.g' + 'a' * 40

    def test_versions_are_isolated_by_channel(self):
        self.assertEqual(publication.destination('stable', '0.13.0'), 'vmango')
        self.assertEqual(publication.destination('devel', self.test_version), 'vmango-devel')
        for channel, version in [('stable', self.test_version), ('devel', '0.13.0'),
                                 ('stable', '0.13.0-rc.1'), ('other', '0.13.0'),
                                 ('devel', '0.13.0~test.123.ginvalid')]:
            with self.subTest(channel=channel, version=version), self.assertRaises(ValueError):
                publication.destination(channel, version)

    def test_devel_queries_do_not_search_stable(self):
        with patch.object(publication, 'entries', return_value=[]) as entries:
            publication.sources(self.test_version, 'jammy', 'vmango-devel')
        self.assertEqual(entries.call_args.args[0], publication.ppa_url('vmango-devel'))

    def test_wrong_ppa_processors_stop_before_upload(self):
        with patch.object(publication, 'get', return_value={'processors_collection_link': 'processors'}), \
             patch.object(publication, 'entries', return_value=[{'name': 'amd64'}, {'name': 'arm64'}]), \
             patch.object(publication.subprocess, 'run') as upload:
            with self.assertRaisesRegex(RuntimeError, 'amd64'):
                publication.ppa(self.test_version, 'jammy', 'vmango-devel')
            upload.assert_not_called()

    def test_mismatched_source_upload_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            changes = pathlib.Path(temp) / 'vmango_source.changes'
            changes.write_text('Source: vmango\nVersion: 0.13.0-1ppa1~jammy\nDistribution: jammy\n')
            publication.validate_changes(changes, '0.13.0', '1', 'jammy')
            for version, revision, series in [(self.test_version, '1', 'jammy'),
                                               ('0.13.0', '2', 'jammy'),
                                               ('0.13.0', '1', 'noble')]:
                with self.subTest(version=version, revision=revision, series=series), self.assertRaises(ValueError):
                    publication.validate_changes(changes, version, revision, series)


class CoprPaginationTest(unittest.TestCase):
    def test_client_list_response_and_multiple_pages(self):
        from copr.v3.helpers import List
        from unittest.mock import Mock
        client = Mock()
        older = {'source_package': {'version': '0.12.0-1.release'}}
        expected = {'id': 42, 'source_package': {'version': '0.13.0-1.release'}}
        client.build_proxy.get_list.side_effect = [List([older] * 100), List([expected])]
        self.assertEqual(publication.copr_builds(client, '0.13.0-1.release'), [expected])
        self.assertEqual(client.build_proxy.get_list.call_args.kwargs['pagination']['offset'], 100)

    def test_unprocessed_source_metadata_is_ignored(self):
        from copr.v3.helpers import List
        from unittest.mock import Mock
        client = Mock()
        client.build_proxy.get_list.return_value = List([{'source_package': None}])
        self.assertEqual(publication.copr_builds(client, '0.13.0-1.release'), [])

    def test_devel_search_uses_devel_project(self):
        from unittest.mock import Mock
        client = Mock()
        client.build_proxy.get_list.return_value = []
        publication.copr_builds(client, '0.13.0~test.123-1.release', 'vmango-devel')
        self.assertEqual(client.build_proxy.get_list.call_args.args[:2], ('subuk', 'vmango-devel'))

    def test_server_metadata_without_dist_suffix_is_reused(self):
        from unittest.mock import Mock
        client = Mock()
        build = {'id': 42, 'source_package': {'version': '0.13.0-1'}}
        client.build_proxy.get_list.return_value = [build]
        self.assertEqual(publication.copr_builds(client, '0.13.0-1.release'), [build])
