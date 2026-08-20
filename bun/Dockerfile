FROM oven/bun:1.3.4-alpine

WORKDIR /app

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

COPY index.ts tsconfig.json ./

ENV PORT=3000
EXPOSE 3000

CMD ["bun", "index.ts"]
