package com.elykia.apihub.data.model

import kotlinx.serialization.Serializable

@Serializable
data class ApiEnvelope<T>(val data: T)

@Serializable
data class ApiErrorEnvelope(val error: ApiErrorBody)

@Serializable
data class ApiErrorBody(
    val code: String,
    val message: String,
    val retryable: Boolean = false,
    val requestId: String = "",
)

@Serializable
data class Capabilities(
    val checkin: Boolean,
    val announcements: Boolean,
    val requiresUserId: Boolean,
)

@Serializable
data class AdapterDescriptor(
    val name: String,
    val displayName: String,
    val capabilities: Capabilities,
)

@Serializable
data class Site(
    val id: String,
    val name: String,
    val baseUrl: String,
    val adapter: String,
    val userId: String,
    val enabled: Boolean,
    val checkinEnabled: Boolean,
    val announcementEnabled: Boolean,
    val checkinCron: String,
    val announcementCron: String,
    val timezone: String,
    val credentialConfigured: Boolean,
    val consecutiveFailures: Int,
    val capabilities: Capabilities,
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class SiteWrite(
    val name: String,
    val baseUrl: String,
    val adapter: String,
    val userId: String,
    val accessToken: String? = null,
    val enabled: Boolean,
    val checkinEnabled: Boolean? = null,
    val announcementEnabled: Boolean? = null,
    val checkinCron: String,
    val announcementCron: String,
    val timezone: String,
)

@Serializable
data class SitePatch(
    val name: String? = null,
    val baseUrl: String? = null,
    val adapter: String? = null,
    val userId: String? = null,
    val accessToken: String? = null,
    val enabled: Boolean? = null,
    val checkinEnabled: Boolean? = null,
    val announcementEnabled: Boolean? = null,
    val checkinCron: String? = null,
    val announcementCron: String? = null,
    val timezone: String? = null,
)

@Serializable
data class CheckinRun(
    val id: String,
    val siteId: String,
    val siteName: String = "",
    val localDate: String,
    val status: String,
    val rewardValue: Long? = null,
    val message: String,
    val errorCode: String? = null,
    val attemptCount: Int,
    val startedAt: String,
    val finishedAt: String? = null,
    val requestId: String,
)

@Serializable
data class Announcement(
    val id: String,
    val siteId: String,
    val siteName: String = "",
    val source: String,
    val fingerprint: String,
    val content: String,
    val kind: String,
    val extra: String? = null,
    val publishedAt: String? = null,
    val firstSeenAt: String,
    val lastSeenAt: String,
    val readAt: String? = null,
)

@Serializable
data class AnnouncementSync(
    val id: String,
    val siteId: String,
    val status: String,
    val addedCount: Int,
    val message: String,
    val startedAt: String,
    val finishedAt: String? = null,
    val requestId: String,
)

@Serializable
data class SiteCounts(val total: Int, val enabled: Int)

@Serializable
data class Summary(
    val sites: SiteCounts,
    val today: Map<String, Int> = emptyMap(),
    val unreadAnnouncements: Int,
)
