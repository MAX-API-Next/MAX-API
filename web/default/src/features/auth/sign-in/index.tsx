/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { Link, useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { NeuralSphere } from '@/components/neural-sphere'
import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()
  const { logo, systemName } = useSystemConfig()
  const canRegister =
    !status?.self_use_mode_enabled && status?.register_enabled !== false

  return (
    <AuthLayout variant='split'>
      <div className='auth-page-grid'>
        <section className='auth-hero-panel'>
          <div className='auth-hero-visual'>
            <NeuralSphere logo={logo} name={systemName} variant='sphere' />
          </div>
        </section>

        <section className='auth-form-panel'>
          <Card className='auth-login-card border-border/60 bg-background/60 backdrop-blur-2xl'>
            <CardHeader className='space-y-4'>
              <Badge variant='outline' className='auth-card-badge w-fit'>
                {t('Sign in')}
              </Badge>
              <div className='space-y-2'>
                <CardTitle className='text-3xl leading-tight font-semibold tracking-tight'>
                  {t('Sign in')}
                </CardTitle>
              </div>
            </CardHeader>

            <CardContent className='space-y-6'>
              {canRegister && (
                <p className='text-muted-foreground text-sm'>
                  {t("Don't have an account?")}{' '}
                  <Link
                    to='/sign-up'
                    className='text-foreground hover:text-primary font-medium underline underline-offset-4'
                  >
                    {t('Sign up')}
                  </Link>
                  .
                </p>
              )}

              <UserAuthForm redirectTo={redirect} className='auth-form-grid' />

              <TermsFooter
                variant='sign-in'
                status={status}
                className='text-muted-foreground text-left text-xs leading-5'
              />
            </CardContent>
          </Card>
        </section>
      </div>
    </AuthLayout>
  )
}
