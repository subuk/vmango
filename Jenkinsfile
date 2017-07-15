#!/usr/bin/env groovy

pipeline {
    agent any

    stages {
        stage('Unit tests') {
            steps {
                node("linux") {
                    checkout scm
                    sh 'make test'
                }
            }
        }
        stage('Integration tests') {
            steps {
                parallel centos_lvm: {
                    node('linux') {
                        checkout scm
                        withEnv([
                            "VMANGO_TEST_TYPE=centos_lvm",
                            "VMANGO_TEST_LIBVIRT_URI=qemu+ssh://centos@192.168.141.3/system",
                        ]){
                            lock('centos@192.168.141.3') {
                                sh '''
                                make test TEST_ARGS='-race -tags integration'
                                '''
                            }
                        }
                    }
                },
                centos_file: {
                    node('linux') {
                        checkout scm
                        withEnv([
                            "VMANGO_TEST_TYPE=centos_file",
                            "VMANGO_TEST_LIBVIRT_URI=qemu+ssh://centos@192.168.141.3/system",
                        ]){
                            lock('centos@192.168.141.3') {
                                sh '''
                                    ssh centos@192.168.141.3 sudo mkdir -p /var/lib/libvirt/images/vmango-vms-test
                                    ssh centos@192.168.141.3 sudo mkdir -p /var/lib/libvirt/images/vmango-images-test
                                    make test TEST_ARGS='-race -tags integration'
                                '''
                            }
                        }
                    }
                },
                debian_lvm: {
                    node('linux') {
                        checkout scm
                        withEnv([
                            "VMANGO_TEST_TYPE=debian_lvm",
                            "VMANGO_TEST_LIBVIRT_URI=qemu+ssh://ubuntu@192.168.141.4/system"
                        ]){
                            lock('ubuntu@192.168.141.4') {
                                sh '''
                                make test TEST_ARGS='-race -tags "integration"'
                                '''
                            }
                        }
                    }
                },
                debian_file: {
                    node('linux') {
                        checkout scm
                        withEnv([
                            "VMANGO_TEST_TYPE=debian_file",
                            "VMANGO_TEST_LIBVIRT_URI=qemu+ssh://ubuntu@192.168.141.4/system"
                        ]){
                            lock('ubuntu@192.168.141.4') {
                                sh '''
                                    ssh ubuntu@192.168.141.4 sudo mkdir -p /var/lib/libvirt/images/vmango-vms-test
                                    ssh ubuntu@192.168.141.4 sudo mkdir -p /var/lib/libvirt/images/vmango-images-test
                                    make test TEST_ARGS='-race -tags "integration"'
                                '''
                            }
                        }
                    }
                }
            }
        }

        stage('Package') {
            steps {
                parallel "Debian 8 Jessie": {
                    node('linux') {
                        checkout scm
                        sh 'make package-debian-8-x64'
                        archiveArtifacts artifacts: 'deb/debian-8-x64/*.deb'
                    }
                },
                "Debian 9 Stretch": {
                    node('linux') {
                        checkout scm
                        sh 'make package-debian-9-x64'
                        archiveArtifacts artifacts: 'deb/debian-9-x64/*.deb'
                    }
                },
                "Ubuntu 14.04 LTS Trusty": {
                    node('linux') {
                        checkout scm
                        sh 'make package-ubuntu-trusty-x64'
                        archiveArtifacts artifacts: 'deb/ubuntu-trusty-x64/*.deb'
                    }
                },
                "Ubuntu 16.04 LTS Xenial": {
                    node('linux') {
                        checkout scm
                        sh 'make package-ubuntu-xenial-x64'
                        archiveArtifacts artifacts: 'deb/ubuntu-xenial-x64/*.deb'
                    }
                },
                "Centos 7": {
                    node('linux') {
                        checkout scm
                        sh 'make package-centos-7-x64'
                        archiveArtifacts artifacts: 'rpm/centos-7-x64-epel/*.rpm', excludes: 'rpm/centos-7-x64-epel/*.src.rpm'
                    }
                }
            }
        }
    }
}
