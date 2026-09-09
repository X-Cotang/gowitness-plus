FROM node:lts-alpine AS buildWeb
COPY ./web /src/web
WORKDIR /src/web
RUN npm ci
RUN npm run build


# gowitness needs go >= 1.26 (required by chromedp/cdproto)
FROM golang:1-bookworm AS buildAPP
LABEL maintainer="XCT"
COPY . /src
COPY --from=buildWeb /src/web/dist /src/web/dist
WORKDIR /src
RUN make docker


# final image
# https://github.com/chromedp/docker-headless-shell#using-as-a-base-image
FROM chromedp/headless-shell:latest AS final

RUN export DEBIAN_FRONTEND=noninteractive \
  && apt-get update \
  && apt-get install -y --no-install-recommends \
  dumb-init fonts-noto fonts-noto-cjk \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/

COPY --from=buildAPP /src/gowitness /usr/local/bin
COPY --from=buildWeb /src/web/dist /data/web/dist
EXPOSE 7171

VOLUME ["/data"]
WORKDIR /data

ENTRYPOINT ["dumb-init", "--"]
