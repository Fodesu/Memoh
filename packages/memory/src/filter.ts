import { db } from '@memoh/db'
import { history } from '@memoh/db/schema'
import { and, gte, lte, asc, eq } from 'drizzle-orm'
import { MemoryUnit } from './memory-unit'

export const filterByTimestamp = async (
  from: Date,
  to: Date,
  user: string,
) => {
  const results = await db
    .select()
    .from(history)
    .where(and(
      gte(history.timestamp, from),
      lte(history.timestamp, to),
      eq(history.user, user),
    ))
    .orderBy(asc(history.timestamp))

  return results.map((result) => ({
    messages: result.messages,
    timestamp: new Date(result.timestamp),
    user: result.user,
  })) as MemoryUnit[]
}
