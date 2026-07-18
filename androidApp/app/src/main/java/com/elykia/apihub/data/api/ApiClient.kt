package com.elykia.apihub.data.api

import com.elykia.apihub.BuildConfig
import com.elykia.apihub.data.model.AdapterDescriptor
import com.elykia.apihub.data.model.Announcement
import com.elykia.apihub.data.model.AnnouncementSync
import com.elykia.apihub.data.model.ApiEnvelope
import com.elykia.apihub.data.model.ApiErrorEnvelope
import com.elykia.apihub.data.model.CheckinRun
import com.elykia.apihub.data.model.Site
import com.elykia.apihub.data.model.SitePatch
import com.elykia.apihub.data.model.SiteWrite
import com.elykia.apihub.data.model.Summary
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.engine.HttpClientEngine
import io.ktor.client.engine.okhttp.OkHttp
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.HttpRequestTimeoutException
import io.ktor.client.plugins.DefaultRequest
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.accept
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.patch
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.contentType
import io.ktor.serialization.kotlinx.json.json
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import java.io.IOException
import java.io.InterruptedIOException
import java.net.URI

class ApiException(
    val status: Int,
    val code: String,
    override val message: String,
    val retryable: Boolean,
    val requestId: String,
) : IOException(message)

class ApiClient(
    rawBaseUrl: String,
    private val token: String,
    engine: HttpClientEngine? = null,
    allowHttp: Boolean = BuildConfig.DEBUG,
    private val requestTimeoutMillis: Long = REQUEST_TIMEOUT_MS,
    private val connectTimeoutMillis: Long = CONNECT_TIMEOUT_MS,
) : AutoCloseable {
    val baseUrl: String = normalizeBaseUrl(rawBaseUrl, allowHttp)

    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = false
    }

    private val client = if (engine == null) {
        HttpClient(OkHttp) { configure() }
    } else {
        HttpClient(engine) { configure() }
    }

    private fun io.ktor.client.HttpClientConfig<*>.configure() {
        expectSuccess = false
        followRedirects = false
        install(DefaultRequest) {
            header(HttpHeaders.Authorization, "Bearer $token")
            accept(ContentType.Application.Json)
        }
        install(ContentNegotiation) { json(json) }
        install(HttpTimeout) {
            requestTimeoutMillis = this@ApiClient.requestTimeoutMillis
            connectTimeoutMillis = this@ApiClient.connectTimeoutMillis
            socketTimeoutMillis = this@ApiClient.requestTimeoutMillis
        }
    }

    suspend fun summary(): Summary = request { get(url("/api/v1/summary")) }

    suspend fun adapters(): List<AdapterDescriptor> =
        request<ApiEnvelope<List<AdapterDescriptor>>> { get(url("/api/v1/site-adapters")) }.data

    suspend fun sites(): List<Site> =
        request<ApiEnvelope<List<Site>>> { get(url("/api/v1/sites")) }.data

    suspend fun createSite(input: SiteWrite): Site =
        request<ApiEnvelope<Site>> {
            post(url("/api/v1/sites")) {
                contentType(ContentType.Application.Json)
                setBody(input)
            }
        }.data

    suspend fun patchSite(id: String, input: SitePatch): Site =
        request<ApiEnvelope<Site>> {
            patch(url("/api/v1/sites/$id")) {
                contentType(ContentType.Application.Json)
                setBody(input)
            }
        }.data

    suspend fun checkins(): List<CheckinRun> =
        request<ApiEnvelope<List<CheckinRun>>> { get(url("/api/v1/checkin-runs?limit=100")) }.data

    suspend fun runCheckin(siteId: String): CheckinRun =
        request<ApiEnvelope<CheckinRun>> { post(url("/api/v1/sites/$siteId/checkin-runs")) }.data

    suspend fun announcements(): List<Announcement> =
        request<ApiEnvelope<List<Announcement>>> { get(url("/api/v1/announcements?limit=100")) }.data

    suspend fun syncAnnouncements(siteId: String): AnnouncementSync =
        request<ApiEnvelope<AnnouncementSync>> {
            post(url("/api/v1/sites/$siteId/announcement-syncs"))
        }.data

    suspend fun setAnnouncementRead(id: String, read: Boolean): Announcement =
        request<ApiEnvelope<Announcement>> {
            patch(url("/api/v1/announcements/$id")) {
                contentType(ContentType.Application.Json)
                setBody(mapOf("read" to read))
            }
        }.data

    private suspend inline fun <reified T> request(
        crossinline call: suspend HttpClient.() -> io.ktor.client.statement.HttpResponse,
    ): T {
        try {
            val response = client.call()
            if (response.status.value !in 200..299) {
                val body = runCatching { response.body<ApiErrorEnvelope>() }.getOrNull()?.error
                throw ApiException(
                    status = response.status.value,
                    code = body?.code ?: "HTTP_ERROR",
                    message = body?.message ?: "HTTP ${response.status.value}",
                    retryable = body?.retryable ?: (response.status.value >= 500),
                    requestId = body?.requestId.orEmpty(),
                )
            }
            return response.body()
        } catch (error: CancellationException) {
            throw error
        } catch (error: ApiException) {
            throw error
        } catch (error: HttpRequestTimeoutException) {
            throw ApiException(0, "UPSTREAM_TIMEOUT", "请求超时，请稍后重试", true, "")
        } catch (error: SerializationException) {
            throw ApiException(0, "INVALID_RESPONSE", "服务器响应格式不兼容", false, "")
        } catch (error: IOException) {
            if (error is InterruptedIOException) {
                throw ApiException(0, "UPSTREAM_TIMEOUT", "请求超时，请稍后重试", true, "")
            }
            throw ApiException(0, "NETWORK_ERROR", "无法连接服务器，请检查网络", true, "")
        }
    }

    private fun url(path: String): String = "$baseUrl$path"

    override fun close() {
        client.close()
    }

    companion object {
        private const val REQUEST_TIMEOUT_MS = 10_000L
        private const val CONNECT_TIMEOUT_MS = 5_000L

        fun normalizeBaseUrl(raw: String, allowHttp: Boolean): String {
            val value = raw.trim().trimEnd('/')
            val uri = runCatching { URI(value) }.getOrElse {
                throw IllegalArgumentException("服务器地址无效")
            }
            val host = uri.host
            require(!host.isNullOrBlank() && uri.userInfo == null) { "服务器地址无效" }
            val scheme = uri.scheme?.lowercase()
            val allowedScheme = scheme == "https" || allowHttp && scheme == "http" && isLocalDevelopmentHost(host)
            require(allowedScheme) {
                if (allowHttp) "服务器地址必须使用 HTTPS；debug HTTP 仅支持本地开发地址" else "服务器地址必须使用 HTTPS"
            }
            require(uri.rawQuery == null && uri.rawFragment == null && (uri.path.isNullOrEmpty() || uri.path == "/")) {
                "服务器地址不能包含路径、查询或片段"
            }
            return value
        }

        private fun isLocalDevelopmentHost(rawHost: String): Boolean {
            val host = rawHost.removePrefix("[").removeSuffix("]").lowercase()
            if (host == "localhost" || host.endsWith(".localhost") || host.endsWith(".local") || host == "::1") {
                return true
            }
            val octets = host.split('.').map { it.toIntOrNull() }
            if (octets.size == 4 && octets.all { it != null && it in 0..255 }) {
                val first = octets[0] ?: return false
                val second = octets[1] ?: return false
                return first == 10 || first == 127 ||
                    first == 169 && second == 254 ||
                    first == 172 && second in 16..31 ||
                    first == 192 && second == 168
            }
            return host.contains(':') && (
                host.startsWith("fc") || host.startsWith("fd") ||
                    host.startsWith("fe8") || host.startsWith("fe9") ||
                    host.startsWith("fea") || host.startsWith("feb")
                )
        }
    }
}
