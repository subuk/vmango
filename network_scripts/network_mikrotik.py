#!/usr/bin/env python
import os
import sys
import ipaddress
import posix, time, md5, binascii, socket, select

#
# Config
#
MIKROTIK_HOST = "192.168.84.1"
SUBNET = ipaddress.ip_network(u"192.168.84.0/24")
START_IP = SUBNET[200]
END_IP = SUBNET[250]


#
# Mikrotik client
# https://wiki.mikrotik.com/wiki/Manual:API#Example_client
#
class ApiRos(object):
    "Routeros api"
    def __init__(self, sk):
        self.sk = sk
        self.currenttag = 0

    def login(self, username, pwd):
        for repl, attrs in self.talk(["/login"]):
            chal = binascii.unhexlify(attrs['=ret'])
        md = md5.new()
        md.update('\x00')
        md.update(pwd)
        md.update(chal)
        self.talk(["/login", "=name=" + username,
                   "=response=00" + binascii.hexlify(md.digest())])

    def talk(self, words):
        if self.writeSentence(words) == 0: return
        r = []
        while 1:
            i = self.readSentence();
            if len(i) == 0: continue
            reply = i[0]
            attrs = {}
            for w in i[1:]:
                j = w.find('=', 1)
                if (j == -1):
                    attrs[w] = ''
                else:
                    attrs[w[:j]] = w[j+1:]
            r.append((reply, attrs))
            if reply == '!done': return r

    def writeSentence(self, words):
        ret = 0
        for w in words:
            self.writeWord(w)
            ret += 1
        self.writeWord('')
        return ret

    def readSentence(self):
        r = []
        while 1:
            w = self.readWord()
            if w == '': return r
            r.append(w)

    def writeWord(self, w):
        # print "<<< " + w
        self.writeLen(len(w))
        self.writeStr(w)

    def readWord(self):
        ret = self.readStr(self.readLen())
        # print ">>> " + ret
        return ret

    def writeLen(self, l):
        if l < 0x80:
            self.writeStr(chr(l))
        elif l < 0x4000:
            l |= 0x8000
            self.writeStr(chr((l >> 8) & 0xFF))
            self.writeStr(chr(l & 0xFF))
        elif l < 0x200000:
            l |= 0xC00000
            self.writeStr(chr((l >> 16) & 0xFF))
            self.writeStr(chr((l >> 8) & 0xFF))
            self.writeStr(chr(l & 0xFF))
        elif l < 0x10000000:
            l |= 0xE0000000
            self.writeStr(chr((l >> 24) & 0xFF))
            self.writeStr(chr((l >> 16) & 0xFF))
            self.writeStr(chr((l >> 8) & 0xFF))
            self.writeStr(chr(l & 0xFF))
        else:
            self.writeStr(chr(0xF0))
            self.writeStr(chr((l >> 24) & 0xFF))
            self.writeStr(chr((l >> 16) & 0xFF))
            self.writeStr(chr((l >> 8) & 0xFF))
            self.writeStr(chr(l & 0xFF))

    def readLen(self):
        c = ord(self.readStr(1))
        if (c & 0x80) == 0x00:
            pass
        elif (c & 0xC0) == 0x80:
            c &= ~0xC0
            c <<= 8
            c += ord(self.readStr(1))
        elif (c & 0xE0) == 0xC0:
            c &= ~0xE0
            c <<= 8
            c += ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
        elif (c & 0xF0) == 0xE0:
            c &= ~0xF0
            c <<= 8
            c += ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
        elif (c & 0xF8) == 0xF0:
            c = ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
            c <<= 8
            c += ord(self.readStr(1))
        return c

    def writeStr(self, str):
        n = 0;
        while n < len(str):
            r = self.sk.send(str[n:])
            if r == 0:
                raise RuntimeError("connection closed by remote end")
            n += r

    def readStr(self, length):
        ret = ''
        while len(ret) < length:
            s = self.sk.recv(length - len(ret))
            if s == '':
                raise RuntimeError("connection closed by remote end")
            ret += s
        return ret


#
# Mikrotik Client tools
#
def client_fetch_lease_database(client):
    leases = []
    client.writeSentence(["/ip/dhcp-server/lease/print"])
    while True:
        response = client.readSentence()
        if response[0] == "!done":
            break
        lease = {}
        for item in response[1:]:
            if not item.startswith("="):
                continue
            key, value = item[1:].split("=", 1)
            # print key, value
            if key == "address":
                lease["address"] = ipaddress.ip_address(value.decode("utf-8"))
            if key == "mac-address":
                lease["mac"] = value
            if key == ".id":
                lease["id"] = value
        if lease.has_key("mac") and lease.has_key("address"):
            leases.append(lease)
    return sorted(leases, key=lambda item: socket.inet_aton(str(item['address'])))

def client_insert_lease(client, address, mac, comment=""):
    client.writeSentence([
        "/ip/dhcp-server/lease/add", "=server=default",
        "=mac-address=%s" % mac, "=address=%s" % address,
        "=comment=%s" % comment,
    ])
    response = client.readSentence()
    if response[0] != "!done":
        raise RuntimeError("lease insert failed: %s" % response)

def client_remove_lease(client, lease):
    client.writeSentence(["/ip/dhcp-server/lease/remove", "=numbers=%s" % lease['id']])
    response = client.readSentence()
    if response[0] != "!done":
        raise RuntimeError("lease removal failed: %s" % response)


#
# Lease database tools
#
def leasedb_find_by_ip(leasedb, ip):
    for entry in leasedb:
        if str(entry['address']) == str(ip):
            return entry
    return None

def leasedb_find_by_mac(leasedb, mac):
    for entry in leasedb:
        if str(entry['mac']).upper() == str(mac).upper():
            return entry
    return None


#
# Actions
#
def lookup(client):
    leasedb = client_fetch_lease_database(client)
    for entry in leasedb:
        if entry["mac"].upper() == os.environ.get("VMANGO_MACHINE_HWADDR", "").upper():
            print(entry["address"])
            return
    print("_unknown_")

def assign(client):
    mac = os.environ["VMANGO_MACHINE_HWADDR"]
    leasedb = client_fetch_lease_database(client)

    current_lease = leasedb_find_by_mac(leasedb, mac)
    if current_lease is not None:
        print(current_lease['address'])
        return

    for ip in SUBNET:
        if ip < START_IP:
            continue
        if ip > END_IP:
            break
        if leasedb_find_by_ip(leasedb, ip) is not None:
            continue
        client_insert_lease(client, ip, mac)
        print(ip)
        return

    print("Error: cannot find next ip address")
    return 1

def release(client):
    mac = os.environ["VMANGO_MACHINE_HWADDR"]
    leasedb = client_fetch_lease_database(client)
    current_lease = leasedb_find_by_mac(leasedb, mac)
    if current_lease is None:
        print("OK: no lease found")
        return
    client_remove_lease(client, current_lease)

def error(client):
    print("Error: unknown action requested")
    return 1


def main():
    handlers = {
        "lookup-ip": lookup,
        "assign-ip": assign,
        "release-ip": release,
    }
    action = sys.argv[1]
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.connect((MIKROTIK_HOST, 8728))
    client = ApiRos(s)
    client.login("admin", "")
    return handlers.get(action, error)(client)



if __name__ == '__main__':
    sys.exit(main() or 0)
