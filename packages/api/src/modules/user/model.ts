import { z } from 'zod'

// 用户角色枚举
const UserRoleSchema = z.enum(['admin', 'member'])

// 创建用户的 Schema
const CreateUserSchema = z.object({
  username: z.string().min(3, 'Username must be at least 3 characters').max(50),
  email: z.string().email('Invalid email format').optional(),
  password: z.string().min(6, 'Password must be at least 6 characters'),
  role: UserRoleSchema.default('member'),
  displayName: z.string().optional(),
  avatarUrl: z.string().url('Invalid URL format').optional(),
})

// 更新用户的 Schema
const UpdateUserSchema = z.object({
  email: z.string().email('Invalid email format').optional(),
  role: UserRoleSchema.optional(),
  displayName: z.string().optional(),
  avatarUrl: z.string().url('Invalid URL format').optional(),
  isActive: z.boolean().optional(),
})

// 更新密码的 Schema
const UpdatePasswordSchema = z.object({
  password: z.string().min(6, 'Password must be at least 6 characters'),
})

export type CreateUserInput = z.infer<typeof CreateUserSchema>
export type UpdateUserInput = z.infer<typeof UpdateUserSchema>
export type UpdatePasswordInput = z.infer<typeof UpdatePasswordSchema>

export const GetUserByIdModel = {
  params: z.object({
    id: z.string().uuid('Invalid user ID format'),
  }),
}

export const CreateUserModel = {
  body: CreateUserSchema,
}

export const UpdateUserModel = {
  params: z.object({
    id: z.string().uuid('Invalid user ID format'),
  }),
  body: UpdateUserSchema,
}

export const DeleteUserModel = {
  params: z.object({
    id: z.string().uuid('Invalid user ID format'),
  }),
}

export const UpdatePasswordModel = {
  params: z.object({
    id: z.string().uuid('Invalid user ID format'),
  }),
  body: UpdatePasswordSchema,
}

