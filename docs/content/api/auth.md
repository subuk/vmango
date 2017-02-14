+++
title = "Authentication"
date = "2017-02-05T16:35:19+03:00"
toc = true
weight = 1

+++

Just send username with `X-Vmango-User` header and password with `X-Vmango-Pass` header:

    curl -H 'X-Vmango-User: admin' -H 'X-Vmango-Pass: secret'  'http://vmango.example.com/api/machines/'
