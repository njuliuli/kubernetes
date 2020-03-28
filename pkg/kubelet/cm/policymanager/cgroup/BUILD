load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "cgroup.go",
        "cgroup_cpu_shares_linux.go",
    ],
    importpath = "k8s.io/kubernetes/pkg/kubelet/cm/policymanager/cgroup",
    visibility = ["//visibility:public"],
    deps = [
        "//staging/src/k8s.io/api/core/v1:go_default_library",
    ] + select({
        "@io_bazel_rules_go//go/platform:android": [
            "//vendor/k8s.io/klog:go_default_library",
        ],
        "@io_bazel_rules_go//go/platform:linux": [
            "//vendor/k8s.io/klog:go_default_library",
        ],
        "//conditions:default": [],
    }),
)

go_test(
    name = "go_default_test",
    srcs = ["cgroup_cpu_shares_linux_test.go"],
    embed = [":go_default_library"],
    deps = select({
        "@io_bazel_rules_go//go/platform:android": [
            "//staging/src/k8s.io/api/core/v1:go_default_library",
            "//vendor/github.com/stretchr/testify/assert:go_default_library",
        ],
        "@io_bazel_rules_go//go/platform:linux": [
            "//staging/src/k8s.io/api/core/v1:go_default_library",
            "//vendor/github.com/stretchr/testify/assert:go_default_library",
        ],
        "//conditions:default": [],
    }),
)
