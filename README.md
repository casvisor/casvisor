# beego-authz [![Build Status](https://travis-ci.org/casbin/beego-authz.svg?branch=master)](https://travis-ci.org/casbin/beego-authz) [![Coverage Status](https://coveralls.io/repos/github/casbin/beego-authz/badge.svg?branch=master)](https://coveralls.io/github/casbin/beego-authz?branch=master) [![GoDoc](https://godoc.org/github.com/casbin/beego-authz?status.svg)](https://godoc.org/github.com/casbin/beego-authz)
A Beego middleware that provides authorization like ACL, RBAC, ABAC based on [casbin](https://github.com/casbin/casbin).

With beego-authz, you can control who can access the resources via which method for your Beego app.

## Get Started

1. Modify the access control policy ``authz_policy.csv`` as you wanted. For example, like below:

```csv
p, alice, /dataset1/*, GET
p, alice, /dataset1/resource1, POST
p, bob, /dataset2/resource2, GET
p, bob, /dataset2/*, POST
```

It means that you want user ``alice`` to access ``/dataset1/*`` via ``GET`` and ``/dataset1/resource1`` via ``POST``. The similar way applies to user ``bob``. For more advanced usage for the policy, please refer to casbin: https://github.com/casbin/casbin

2. Insert the authorizer as a Beego filter.

```golang
beego.InsertFilter("*", beego.BeforeRouter, authz.NewAuthorizer(casbin.NewEnforcer("authz_model.conf", "authz_policy.csv")))
```

## Note

You need to have authentication enabled before using casbin authorization module, because authorization needs to have a user name to enforce the policy. And authentication will give us a user name. Currently, the built-in HTTP basic authentication is fully supported.

## License

This project is licensed under the [Apache 2.0 license](https://github.com/casbin/beego-authz/blob/master/LICENSE).
