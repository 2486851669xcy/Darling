FROM golang:1.22-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/darling .

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build --chown=nonroot:nonroot /out/darling /app/darling
COPY --chown=nonroot:nonroot web /app/web
COPY --chown=nonroot:nonroot data /app/data

ENV PORT=8080
ENV DATABASE_PATH=/app/data/runtime/dimension.db

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/app/darling"]
