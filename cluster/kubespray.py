#!/usr/bin/python3
import sys
import re
import pdb
import subprocess

KUBE_ROOT = "/home/li/go/src/k8s.io/kubernetes"
DOWNLOAD_CACHE_DIR = "/tmp/kubespray_cache"
KUBE_DOWNLOAD_CONFIG = "/home/li/kubespray/inventory/apollo/group_vars/k8s-cluster/k8s-cluster.yml"


def replaceLine(version, filename, checksum):
    with open(KUBE_DOWNLOAD_CONFIG, 'r') as f:
        content = f.read()
        sFrom = r"{}: \S+ #{}_checksums\n".format(version, filename)
        sTo = r"{}: {} #{}_checksums\n".format(version, checksum, filename)
        content = re.sub(sFrom, sTo, content)

    with open(KUBE_DOWNLOAD_CONFIG, 'w') as f:
        f.write(content)

    return


if __name__ == '__main__':
    if len(sys.argv) > 1:
        version = sys.argv[1]
    else:
        version = "v1.17.10"

    # cache directory will be removed after host reboot
    subprocess.run(["mkdir", "-p", "{}/images".format(DOWNLOAD_CACHE_DIR)])

    for binary in ["kubelet", "kubectl", "kubeadm"]:
        # move file
        fFrom = "{}/_output/release-stage/server/linux-amd64/kubernetes/server/bin/{}".format(
            KUBE_ROOT, binary)
        fTo = "{}/{}-{}-amd64".format(DOWNLOAD_CACHE_DIR, binary, version)
        subprocess.run(["rsync", "-a", fFrom, fTo])
        subprocess.run(["chmod", "755", fTo])
        # get sha256 checksum
        result = subprocess.run(
            ["sha256sum", fTo], check=True, stdout=subprocess.PIPE)
        checksum = result.stdout.split()[0].decode("utf-8")
        replaceLine(version, binary, checksum)

    for image in ["kube-apiserver", "kube-controller-manager",
                  "kube-proxy", "kube-scheduler"]:
        fFrom = "{}/_output/release-stage/server/linux-amd64/kubernetes/server/bin/{}.tar".format(
            KUBE_ROOT, image)
        fTo = "{}/images/gcr.io_google-containers_{}_{}.tar".format(
            DOWNLOAD_CACHE_DIR, image, version)
        subprocess.run(["rsync", "-a", fFrom, fTo])
        subprocess.run(["chmod", "755", fTo])
