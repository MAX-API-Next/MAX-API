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
import { SystemInfoSection } from '../general/system-info-section'
import {
  parseHeaderNavAccessModule,
  parseHeaderNavModules,
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '../maintenance/config'
import { HeaderNavigationSection } from '../maintenance/header-navigation-section'
import { NoticeSection } from '../maintenance/notice-section'
import { RankingsVisibilitySection } from '../maintenance/rankings-visibility-section'
import { SidebarModulesSection } from '../maintenance/sidebar-modules-section'
import type { SiteSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'

const SITE_SECTIONS = [
  {
    id: 'system-info',
    titleKey: 'System Information',
    build: (settings: SiteSettings) => (
      <SystemInfoSection
        defaultValues={{
          theme: {
            frontend: 'default',
          },
          SystemName: settings.SystemName,
          Logo: settings.Logo,
          Footer: settings.Footer,
          About: settings.About,
          HomePageContent: settings.HomePageContent,
          ServerAddress: settings.ServerAddress,
          legal: {
            user_agreement: settings['legal.user_agreement'],
            privacy_policy: settings['legal.privacy_policy'],
          },
        }}
      />
    ),
  },
  {
    id: 'notice',
    titleKey: 'System Notice',
    build: (settings: SiteSettings) => (
      <NoticeSection defaultValue={settings.Notice ?? ''} />
    ),
  },
  {
    id: 'rankings',
    titleKey: 'Rankings',
    build: (settings: SiteSettings) => {
      const headerNavConfig = parseHeaderNavModules(settings.HeaderNavModules)
      const rankingsConfig = parseHeaderNavAccessModule(
        settings.RankingsModule,
        headerNavConfig.rankings
      )
      return <RankingsVisibilitySection config={rankingsConfig} />
    },
  },
  {
    id: 'header-navigation',
    titleKey: 'Header navigation',
    build: (settings: SiteSettings) => {
      const headerNavConfig = parseHeaderNavModules(settings.HeaderNavModules)
      return <HeaderNavigationSection config={headerNavConfig} />
    },
  },
  {
    id: 'sidebar-modules',
    titleKey: 'Sidebar modules',
    build: (settings: SiteSettings) => {
      const sidebarConfig = parseSidebarModulesAdmin(
        settings.SidebarModulesAdmin
      )
      const sidebarSerialized = serializeSidebarModulesAdmin(sidebarConfig)
      return (
        <SidebarModulesSection
          config={sidebarConfig}
          initialSerialized={sidebarSerialized}
        />
      )
    },
  },
] as const

export type SiteSectionId = (typeof SITE_SECTIONS)[number]['id']

const siteRegistry = createSectionRegistry<SiteSectionId, SiteSettings>({
  sections: SITE_SECTIONS,
  defaultSection: 'system-info',
  basePath: '/system-settings/site',
  urlStyle: 'path',
})

export const SITE_SECTION_IDS = siteRegistry.sectionIds
export const SITE_DEFAULT_SECTION = siteRegistry.defaultSection
export const getSiteSectionNavItems = siteRegistry.getSectionNavItems
export const getSiteSectionContent = siteRegistry.getSectionContent
export const getSiteSectionMeta = siteRegistry.getSectionMeta
