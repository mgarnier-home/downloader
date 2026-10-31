# Étape 1 : build du front Angular.
FROM node:24 AS front
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npx ng build

# Étape 2 : build du binaire Go, qui embarque le front.
FROM golang:1.26 AS back
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=front /src/frontend/dist/downloader/browser/ ./web/browser/
# Déclarés après le téléchargement des modules pour ne pas invalider leur cache.
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags="-X 'github.com/mgarnier-home/utils/buildinfo.Version=${VERSION}' -X 'github.com/mgarnier-home/utils/buildinfo.Commit=${COMMIT}' -X 'github.com/mgarnier-home/utils/buildinfo.Date=${DATE}'" \
    -o /downloader .

# Étape 3 : image finale, sans utilisateur imposé (l'utilisateur se choisit au déploiement).
FROM gcr.io/distroless/static
COPY --from=back /downloader /downloader
EXPOSE 8080
ENTRYPOINT ["/downloader"]
