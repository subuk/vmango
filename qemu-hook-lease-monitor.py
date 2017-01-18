#!/usr/bin/env python
#
#  Runned by libvirt as the following:
#  /etc/libvirt/hooks/qemu guest_name start begin -
#
import sys
import json
import logging
import logging.handlers
import argparse
import subprocess as sp
import xml.dom.minidom as minidom

DEBUG = False

logger = logging.getLogger(__name__)

parser = argparse.ArgumentParser()
parser.add_argument("guest_name")
parser.add_argument("event")
parser.add_argument("direction")
parser.add_argument("input", type=argparse.FileType('r'))


def configure_logging():
    syslog_handler = logging.handlers.SysLogHandler('/dev/log')
    syslog_handler.setFormatter(
        logging.Formatter("vmango-dhcp-lease-monitor[%(process)d]: %(message)s")
    )
    logger.addHandler(syslog_handler)
    logger.setLevel(logging.INFO)

    if DEBUG:
        logger.setLevel(logging.DEBUG)
        logger.addHandler(logging.StreamHandler())


def get_leases(network):
    # Old libvirt version
    interface = None
    with open("/var/lib/libvirt/dnsmasq/%s.conf" % network) as netcfg:
        for line in netcfg:
            items = line.strip().split('=')
            if len(items) <= 1 or len(items) > 2:
                continue
            key, value = items
            if key == "interface":
                interface = value
            if key != "dhcp-leasefile":
                continue
            with open(value) as leasedb:
                db = {}
                for line in leasedb:
                    data = line.strip().split()
                    if not data:
                        continue
                    mac = data[1]
                    ip = data[2]
                    db[mac] = ip
                return db, interface

    # New libvirt version
    if interface is None:
        raise RuntimeError("Cannot find interface for network %s" % network)

    with open("/var/lib/libvirt/dnsmasq/%s.status" % interface) as leasedb:
        db = {}
        for item in json.load(leasedb):
            mac = item["mac-address"]
            ip = item["ip-address"]
            db[mac] = ip
        return db, interface


def main():
    args = parser.parse_args()
    configure_logging()
    logger.debug("started with arguments: %s", sys.argv)

    if args.event != "stopped" or args.direction != "end":
        logger.debug("bad event or direction, exiting")
        return

    domain_config = minidom.parse(args.input)
    interfaces = domain_config.getElementsByTagName("interface")
    for interface in interfaces:
        if interface.attributes["type"].nodeValue != "network":
            continue
        mac = interface.getElementsByTagName(
            "mac"
        )[0].attributes["address"].nodeValue
        network = interface.getElementsByTagName(
            "source"
        )[0].attributes["network"].nodeValue
        leases, bridge = get_leases(network)
        ip = leases.get(mac)
        if ip is None:
            logger.debug(
                "cannot find machine (%s) mac (%s) in dhcp leases database",
                args.guest_name, mac,
            )
            continue

        cmd = ["dhcp_release", bridge, ip, mac]
        logger.info(
            "removing dhcp lease for machine %s with: %s",
            args.guest_name, " ".join(cmd)
        )
        sp.call(cmd)

if __name__ == '__main__':
    main()
