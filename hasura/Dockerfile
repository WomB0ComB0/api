FROM hasura/graphql-engine:v2.43.0.cli-migrations-v3

COPY metadata /hasura-metadata

ENV HASURA_GRAPHQL_METADATA_DIR=/hasura-metadata \
    HASURA_GRAPHQL_SERVER_HOST=* \
    HASURA_GRAPHQL_ENABLE_TELEMETRY=false \
    HASURA_GRAPHQL_UNAUTHORIZED_ROLE=anonymous

EXPOSE 8080
