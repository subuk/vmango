import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location('publish_packages', pathlib.Path(__file__).with_name('publish-packages.py'))
publication = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publication)


class PublicationStateTest(unittest.TestCase):
    def setUp(self):
        self.source = {'status': 'Published'}
        self.build = {'self_link': 'build/1', 'web_link': 'https://example.com/build/1',
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
