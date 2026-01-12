import { db } from '@memoh/db'
import { history } from '@memoh/db/schema'
import { eq, desc, and, gte, lte, asc } from 'drizzle-orm'

export const getMemoryMessages = async (
  userId: string,
  query: {
    limit: number
    page: number
  }
) => {
  const { limit, page } = query
  const results = await db
    .select()
    .from(history)
    .where(eq(history.user, userId))
    .orderBy(desc(history.timestamp))
    .limit(limit)
    .offset((page - 1) * limit)

  return results
}

export const getMemoryMessagesFilter = async (
  userId: string,
  query: {
    from: Date
    to: Date
  }
) => {
  const { from, to } = query
  const results = await db
    .select()
    .from(history)
    .where(and(
      gte(history.timestamp, from),
      lte(history.timestamp, to),
      eq(history.user, userId),
    ))
    .orderBy(asc(history.timestamp))

  return results
}