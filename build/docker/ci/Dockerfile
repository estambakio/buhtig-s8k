FROM microsoft/azure-cli:2.0.51

LABEL maintainer="egor.stambakio@opuscapita.com"

RUN apk add -v --no-cache make grep sed curl git bash tar unzip \
	&& rm -rf /root/.cache

# install kubectl
RUN curl -fLk -o /usr/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.14.1/bin/linux/amd64/kubectl \
	&& chmod +x /usr/bin/kubectl

# install vault
RUN curl -fsSLk -o /tmp/vault.zip https://releases.hashicorp.com/vault/1.1.0/vault_1.1.0_linux_amd64.zip \
  && unzip /tmp/vault.zip -d /usr/local/bin/ \
  && rm -f /tmp/vault.zip

# install ytt
RUN curl -fsSLk -o /usr/local/bin/ytt https://github.com/k14s/ytt/releases/download/v0.14.0/ytt-linux-amd64 \
  && chmod +x /usr/local/bin/ytt
