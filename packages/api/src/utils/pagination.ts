/**
 * 分页参数接口
 */
export interface PaginationParams {
  page?: number
  limit?: number
  sortBy?: string
  sortOrder?: 'asc' | 'desc'
}

/**
 * 分页结果接口
 */
export interface PaginatedResult<T> {
  items: T[]
  pagination: {
    page: number
    limit: number
    total: number
    totalPages: number
    hasNext: boolean
    hasPrev: boolean
  }
}

/**
 * 解析分页参数
 */
export function parsePaginationParams(query: Record<string, any>): Required<PaginationParams> {
  const page = Math.max(1, parseInt(query.page as string) || 1)
  const limit = Math.min(100, Math.max(1, parseInt(query.limit as string) || 10))
  const sortBy = query.sortBy as string || 'createdAt'
  const sortOrder = (query.sortOrder as string)?.toLowerCase() === 'desc' ? 'desc' : 'asc'

  return {
    page,
    limit,
    sortBy,
    sortOrder,
  }
}

/**
 * 创建分页结果
 */
export function createPaginatedResult<T>(
  items: T[],
  total: number,
  page: number,
  limit: number
): PaginatedResult<T> {
  const totalPages = Math.ceil(total / limit)
  
  return {
    items,
    pagination: {
      page,
      limit,
      total,
      totalPages,
      hasNext: page < totalPages,
      hasPrev: page > 1,
    },
  }
}

/**
 * 计算偏移量
 */
export function calculateOffset(page: number, limit: number): number {
  return (page - 1) * limit
}

