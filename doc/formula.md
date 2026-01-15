LLAR Formula
====

```coffee
id "DaveGamble/cJSON"  # module id

fromVer "v1.0.0"  # run formula from this version

onRequire (proj, deps) => {  # extract deps from this project
    cmake := proj.readFile("CMakeLists.txt")

    # find_package(re2c 2.0 REQUIRED)  -> {name: "re2c", version: "2.0"}
    # find_package(zlib REQUIRED)      -> {name: "zlib", version: ""}
    matches := findDeps(cmake)  # return [{name: "re2c", version: "2.0"}, {name: "zlib", version: ""}]

    for m in matches {
        if m.Version == "" {
            ...
        }
        deps.require(modID(m.Name), m.Version)
    }
}

onBuild (ctx, proj, out) => {  # build this project
    ...
}
```
