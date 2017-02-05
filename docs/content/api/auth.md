+++
title = "Authentication"
date = "2017-02-05T16:35:19+03:00"
toc = true
weight = 1

+++

Get 'vmango' cookie from login page

    curl -v --data 'username=USER&password=SECRET' -X POST  'http://vmango.example.com/login/'  2>&1  |grep -i Set-Cookie
    < Set-Cookie: vmango=MTQ4NjMw...; Path=/; Expires=Tue, 07 Mar 2017 13:40:26 GMT; Max-Age=2592000

Send this cookie with each request, e.g.

    curl -H 'Cookie: vmango=MTQ4NjM...' "http://vmango.example.com/machines"
