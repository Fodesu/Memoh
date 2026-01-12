import { db } from '@memoh/db'
import { schedule } from '@memoh/db/schema'
import { ChatModel, EmbeddingModel, Schedule } from '@memoh/shared'
import { eq, desc, asc, and, sql } from 'drizzle-orm'
import cron from 'node-cron'
import { createAgent } from '../agent/service'
import { getChatModel, getEmbeddingModel, getSummaryModel } from '../model/service'
import { getSettings } from '../settings/service'
import { calculateOffset, createPaginatedResult, type PaginatedResult } from '../../utils/pagination'
import type { CreateScheduleInput, UpdateScheduleInput } from './model'

/**
 * Schedule 列表返回类型
 */
type ScheduleListItem = {
  id: string
  name: string
  description: string
  command: string
  pattern: string
  maxCalls: number | null
  user: string
  createdAt: Date
  updatedAt: Date
  active: boolean
}


/**
 * 获取用户的所有 schedules（支持分页）
 */
export const getSchedules = async (
  userId: string,
  params?: {
    limit?: number
    page?: number
    sortOrder?: 'asc' | 'desc'
  }
): Promise<PaginatedResult<ScheduleListItem>> => {
  const limit = params?.limit || 10
  const page = params?.page || 1
  const sortOrder = params?.sortOrder || 'desc'
  const offset = calculateOffset(page, limit)

  // 获取总数
  const [{ count }] = await db
    .select({ count: sql<number>`count(*)` })
    .from(schedule)
    .where(eq(schedule.user, userId))

  // 获取分页数据
  const orderFn = sortOrder === 'desc' ? desc : asc
  const schedules = await db
    .select()
    .from(schedule)
    .where(eq(schedule.user, userId))
    .orderBy(orderFn(schedule.createdAt))
    .limit(limit)
    .offset(offset)

  return createPaginatedResult(schedules, Number(count), page, limit)
}

export const getActiveSchedules = async (
  userId?: string
) => {
  const schedules = await db
    .select().from(schedule)
    .where(and(...[
      userId ? eq(schedule.user, userId) : undefined,
      eq(schedule.active, true),
    ]))
    .orderBy(desc(schedule.createdAt))
  return schedules
}

/**
 * 根据 ID 获取单个 schedule
 */
export const getSchedule = async (
  scheduleId: string
) => {
  const [result] = await db
    .select()
    .from(schedule)
    .where(eq(schedule.id, scheduleId))
  return result
}

/**
 * 创建新的 schedule
 */
export const createSchedule = async (
  userId: string,
  data: CreateScheduleInput
) => {
  const { scheduleTask } = createScheduler()
  const [newSchedule] = await db
    .insert(schedule)
    .values({
      user: userId,
      name: data.name,
      description: data.description,
      command: data.command,
      pattern: data.pattern,
      maxCalls: data.maxCalls || null,
      active: true,
    })
    .returning()

  scheduleTask(userId, {
    id: newSchedule.id!,
    pattern: newSchedule.pattern,
    name: newSchedule.name,
    description: newSchedule.description,
    command: newSchedule.command,
    maxCalls: newSchedule.maxCalls || undefined,
  })
  
  return newSchedule
}

/**
 * 更新 schedule
 */
export const updateSchedule = async (
  scheduleId: string,
  userId: string,
  data: UpdateScheduleInput
) => {
  // 检查 schedule 是否存在且属于该用户
  const existingSchedule = await getSchedule(scheduleId)
  if (!existingSchedule) {
    return null
  }
  
  if (existingSchedule.user !== userId) {
    throw new Error('Forbidden: You do not have permission to update this schedule')
  }

  const [updatedSchedule] = await db
    .update(schedule)
    .set({
      ...data,
      updatedAt: new Date(),
    })
    .where(eq(schedule.id, scheduleId))
    .returning()
  
  return updatedSchedule
}

/**
 * 删除 schedule
 */
export const deleteSchedule = async (
  scheduleId: string,
  userId: string
) => {
  // 检查 schedule 是否存在且属于该用户
  const existingSchedule = await getSchedule(scheduleId)
  if (!existingSchedule) {
    return null
  }
  
  if (existingSchedule.user !== userId) {
    throw new Error('Forbidden: You do not have permission to delete this schedule')
  }

  const [deletedSchedule] = await db
    .delete(schedule)
    .where(eq(schedule.id, scheduleId))
    .returning()
  
  return deletedSchedule
}

export const setMaxCalls = async (
  scheduleId: string,
  maxCalls: number
) => {
  await db
    .update(schedule)
    .set({ maxCalls })
    .where(eq(schedule.id, scheduleId))
}

export const setActive = async (
  scheduleId: string,
  active: boolean
) => {
  await db
    .update(schedule)
    .set({ active })
    .where(eq(schedule.id, scheduleId))
}

export const createScheduler = () => {
  const scheduleTask = (userId: string, schedule: Schedule) => {
    const task = cron.schedule(schedule.pattern, async () => {
      const [chatModel, embeddingModel, summaryModel, userSettings] = await Promise.all([
        getChatModel(userId),
        getEmbeddingModel(userId),
        getSummaryModel(userId),
        getSettings(userId),
      ])
      if (!chatModel || !embeddingModel || !summaryModel) {
        throw new Error('Model configuration not found. Please configure your models in settings.')
      }
      const agent = await createAgent({
        userId,
        chatModel: chatModel.model as ChatModel,
        embeddingModel: embeddingModel.model as EmbeddingModel,
        summaryModel: summaryModel.model as ChatModel,
        maxContextLoadTime: userSettings?.maxContextLoadTime || undefined,
        language: userSettings?.language || undefined,
      })
      await agent.triggerSchedule(schedule)
    }, {
      maxExecutions: schedule.maxCalls || undefined,
    })
    task.on('execution:finished', async () => {
      const { maxCalls } = await getSchedule(schedule.id!)
      if (maxCalls) {
        setMaxCalls(schedule.id!, maxCalls - 1)
        if (maxCalls - 1 === 0) {
          await setActive(schedule.id!, false)
        }
      }
    })
    task.on('execution:maxReached', async () => {
      await setActive(schedule.id!, false)
    })
  }
  
  const resume = async () => {
    const schedules = await getActiveSchedules()
    for (const schedule of schedules) {
      scheduleTask(schedule.user, {
        id: schedule.id!,
        pattern: schedule.pattern,
        name: schedule.name,
        description: schedule.description,
        command: schedule.command,
        maxCalls: schedule.maxCalls || undefined,
      })
    }
  }

  return {
    scheduleTask,
    resume,
  }
}
