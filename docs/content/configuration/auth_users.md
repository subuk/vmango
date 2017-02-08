+++
weight = 15
title = "Users"
date = "2017-02-05T17:49:46+03:00"
toc = true
+++

User passwords stored in config file in hashed form (golang.org/x/crypto/bcrypt). For adding new user or change password for existing, generate a new one with `vmango genpw` utility:

    vmango genpw plainsecret

Copy output and insert into config file:
       
    ...
    user "admin" {
        password = "$2a$10$uztHNVBxZ08LBmboJpAqguN4gZSymgmjaJ2xPHKwAqH.ukgaplb96"
    }
    ...
