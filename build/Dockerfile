FROM ubuntu:16.04
LABEL maintainers="Kubernetes Authors"
LABEL description="Baidu Cloud CSI Plugin"

RUN apt-get update && apt-get install -y parted && apt-get install -y xfsprogs
ADD output/csi-plugin /usr/bin/csi-plugin
RUN chmod +x /usr/bin/csi-plugin

ENTRYPOINT ["csi-plugin"]