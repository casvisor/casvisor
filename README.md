# beeauthz
A Beego sample project that uses [casbin](https://github.com/hsluoyz/casbin) as the authorization module.

With casbin, you can control who can access the resources via which method for your Beego app.

## Get Started

1. Modify the access control policy ``authz_policy.csv`` as you wanted. For example, like below:

```csv
p, alice, /dataset1/*, GET
p, alice, /dataset1/resource1, POST
p, bob, /dataset2/resource2, GET
p, bob, /dataset2/*, POST
```

It means that you want user ``alice`` to access ``/dataset1/*`` via ``GET`` and ``/dataset1/resource1`` via ``POST``. The similar way applies to user ``bob``. For more advanced usage for the policy, please refer to casbin: https://github.com/hsluoyz/casbin

2. Insert the authorizer as a Beego filter.

```golang
beego.InsertFilter("*", beego.BeforeRouter, authz.NewBasicAuthorizer())
```

## Note

You need to have authentication enabled before using casbin authorization module, because authorization needs to have a user name to enforce the policy. And authentication will give us a user name. Currently, the built-in HTTP basic authentication is fully supported.

## License

This project is licensed under the [Apache 2.0 license](https://github.com/hsluoyz/casbin/blob/master/LICENSE).

## Contact

If you have any issues or feature requests, please feel free to contact me at:
- https://github.com/hsluoyz/casbin/issues
- hsluoyz@gmail.com (Yang Luo's email, if your issue needs to be kept private, please contact me via this mail)
