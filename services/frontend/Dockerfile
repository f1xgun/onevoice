FROM node:20-alpine AS deps
WORKDIR /app
RUN npm install -g pnpm@9
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM node:20-alpine AS builder
WORKDIR /app
RUN npm install -g pnpm@9
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
ARG API_URL=http://api:8080
ARG NEXT_PUBLIC_API_URL=/api/v1
ARG PUBLIC_URL=http://localhost
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV PUBLIC_URL=$PUBLIC_URL
ARG ORCHESTRATOR_URL=http://orchestrator:8090
ENV API_URL=$API_URL
ENV ORCHESTRATOR_URL=$ORCHESTRATOR_URL
# 152-ФЗ Art. 14 data-controller identity, inlined into the client bundle at
# build time. Setting APP_ENV=production makes `pnpm build` hard-fail (via
# scripts/check-legal-entity.mjs) when any value is unset or a placeholder, so a
# production image can never ship the «[Юридическое лицо — будет обновлено]»
# stub. Pass real values: docker build --build-arg APP_ENV=production
#   --build-arg NEXT_PUBLIC_LEGAL_ENTITY_NAME="ООО …" … (see .env.example §13).
ARG APP_ENV
ARG NEXT_PUBLIC_LEGAL_ENTITY_NAME
ARG NEXT_PUBLIC_LEGAL_INN
ARG NEXT_PUBLIC_LEGAL_ADDRESS
ARG NEXT_PUBLIC_LEGAL_EMAIL_PDN
ENV APP_ENV=$APP_ENV
ENV NEXT_PUBLIC_LEGAL_ENTITY_NAME=$NEXT_PUBLIC_LEGAL_ENTITY_NAME
ENV NEXT_PUBLIC_LEGAL_INN=$NEXT_PUBLIC_LEGAL_INN
ENV NEXT_PUBLIC_LEGAL_ADDRESS=$NEXT_PUBLIC_LEGAL_ADDRESS
ENV NEXT_PUBLIC_LEGAL_EMAIL_PDN=$NEXT_PUBLIC_LEGAL_EMAIL_PDN
RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV NEXT_TELEMETRY_DISABLED=1
RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
