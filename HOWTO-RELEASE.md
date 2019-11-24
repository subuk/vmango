# How to release

1. Bump version in Makefile
2. Add changelog entries to vmango.spec.in and debian/changelog
3. `git commit -m 'Release X.X.X'`
4. `git tag vX.X.X`
5. Build source rpm and upload to COPR from web interface `./dockerbuild.sh centos-7 make srpm`
6. Build sdeb
    1. Build source deb for bionic: `/dockerbuild.sh ubuntu-1804 make sdeb DEB_DISTRIBUTION=bionic`
    2. Sign: `/dockerbuild.sh ubuntu-1804 debsign -k XXX vmango_X.X.X-1_source.changes`
    3. Upload: `dput ppa:subuk/vmango vmango_X.X.X-1_source.changes`
7. `git push origin master --tags`
8. `git push lp master --tags`
9. Create github release from git tag
