import Elysia from 'elysia'
import { adminMiddleware } from '../../middlewares'
import {
  GetUserByIdModel,
  CreateUserModel,
  UpdateUserModel,
  DeleteUserModel,
  UpdatePasswordModel,
} from './model'
import {
  getUsers,
  getUserById,
  createUser,
  updateUser,
  deleteUser,
  updateUserPassword,
} from './service'

export const userModule = new Elysia({
  prefix: '/user',
})
  // 使用管理员中间件保护所有路由
  .use(adminMiddleware)
  // Get all users
  .get('/', async ({ query }) => {
    try {
      const page = parseInt(query.page as string) || 1
      const limit = parseInt(query.limit as string) || 10
      const sortBy = query.sortBy as string || 'createdAt'
      const sortOrder = (query.sortOrder as string) || 'desc'

      const result = await getUsers({
        page,
        limit,
        sortBy,
        sortOrder: sortOrder as 'asc' | 'desc',
      })
      
      return {
        success: true,
        ...result,
      }
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch users',
      }
    }
  })
  // Get user by ID
  .get('/:id', async ({ params, set }) => {
    try {
      const { id } = params
      const user = await getUserById(id)
      
      if (!user) {
        set.status = 404
        return {
          success: false,
          error: 'User not found',
        }
      }

      return {
        success: true,
        data: user,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch user',
      }
    }
  }, GetUserByIdModel)
  // Create new user
  .post('/', async ({ body, set }) => {
    try {
      const newUser = await createUser(body)
      set.status = 201
      return {
        success: true,
        data: newUser,
      }
    } catch (error) {
      if (error instanceof Error && (
        error.message.includes('already exists')
      )) {
        set.status = 409
      } else {
        set.status = 500
      }
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to create user',
      }
    }
  }, CreateUserModel)
  // Update user
  .put('/:id', async ({ params, body, set }) => {
    try {
      const { id } = params
      const updatedUser = await updateUser(id, body)
      
      if (!updatedUser) {
        set.status = 404
        return {
          success: false,
          error: 'User not found',
        }
      }

      return {
        success: true,
        data: updatedUser,
      }
    } catch (error) {
      if (error instanceof Error && error.message.includes('already exists')) {
        set.status = 409
      } else {
        set.status = 500
      }
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to update user',
      }
    }
  }, UpdateUserModel)
  // Delete user
  .delete('/:id', async ({ params, set }) => {
    try {
      const { id } = params
      const deletedUser = await deleteUser(id)
      
      if (!deletedUser) {
        set.status = 404
        return {
          success: false,
          error: 'User not found',
        }
      }

      return {
        success: true,
        data: deletedUser,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to delete user',
      }
    }
  }, DeleteUserModel)
  // Update user password
  .patch('/:id/password', async ({ params, body, set }) => {
    try {
      const { id } = params
      const updatedUser = await updateUserPassword(id, body.password)
      
      if (!updatedUser) {
        set.status = 404
        return {
          success: false,
          error: 'User not found',
        }
      }

      return {
        success: true,
        data: updatedUser,
        message: 'Password updated successfully',
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to update password',
      }
    }
  }, UpdatePasswordModel)

