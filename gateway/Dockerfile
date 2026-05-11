FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/backstop-gateway .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/backstop-gateway /usr/local/bin/backstop-gateway
ENTRYPOINT ["/usr/local/bin/backstop-gateway"]

