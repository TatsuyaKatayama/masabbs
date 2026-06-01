# Build stage
FROM node:22-alpine AS builder

WORKDIR /app

# Install dependencies
COPY package*.json ./
RUN npm install

# Copy source code
COPY . .

# Build the application
# We assume NEXT_PUBLIC_API_URL might be needed at build time if using SSG/ISR
# but for a reverse proxy setup, we can often use relative paths.
RUN npm run build

# Final stage
FROM node:22-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production

# Copy necessary files for standalone output
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static

# Expose the port
EXPOSE 3000

# Command to run the application
CMD ["node", "server.js"]
