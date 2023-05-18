FROM gcr.io/distroless/static

# Copy the binary that goreleaser built
COPY node-resolver /node-resolver

# Run the web service on container startup.
ENTRYPOINT ["/node-resolver"]
CMD ["serve"]
