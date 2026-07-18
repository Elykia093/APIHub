package com.elykia.apihub.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.elykia.apihub.data.model.AdapterDescriptor
import com.elykia.apihub.data.model.Announcement
import com.elykia.apihub.data.model.CheckinRun
import com.elykia.apihub.data.model.Site
import java.time.DateTimeException
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import top.yukonga.miuix.kmp.theme.MiuixTheme

@Composable
fun ApiHubApp(viewModel: MainViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    APIHubTheme {
        if (state.restoring) {
            LoadingScreen()
        } else if (!state.connected) {
            ConnectScreen(state.error, state.loading, viewModel::connect)
        } else {
            MainShell(state, viewModel)
        }
    }
}

@Composable
private fun LoadingScreen() {
    Box(
        modifier = Modifier.fillMaxSize().safeDrawingPadding(),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(color = MiuixTheme.colorScheme.primary)
    }
}

@Composable
private fun ConnectScreen(error: String?, loading: Boolean, onConnect: (String, String) -> Unit) {
    var serverUrl by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    Column(
        modifier = Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        ApiText("APIHub", fontSize = 32.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(8.dp))
        ApiText("连接自托管服务器", color = MiuixTheme.colorScheme.onBackgroundVariant)
        Spacer(Modifier.height(28.dp))
        ApiCard(modifier = Modifier.fillMaxWidth()) {
            ApiTextField(serverUrl, { serverUrl = it }, "服务器地址", Modifier.fillMaxWidth())
            Spacer(Modifier.height(12.dp))
            ApiTextField(
                value = token,
                onValueChange = { token = it },
                label = "管理员令牌",
                modifier = Modifier.fillMaxWidth(),
                visualTransformation = PasswordVisualTransformation(),
            )
            Spacer(Modifier.height(18.dp))
            ApiPrimaryButton(
                text = if (loading) "连接中" else "连接",
                onClick = { onConnect(serverUrl, token) },
                modifier = Modifier.fillMaxWidth(),
                enabled = !loading && serverUrl.isNotBlank() && token.isNotBlank(),
            )
        }
        if (!error.isNullOrBlank()) {
            Spacer(Modifier.height(12.dp))
            ErrorBanner(error)
        }
        Spacer(Modifier.height(20.dp))
        ApiText(
            "Release 仅允许 HTTPS；debug 构建可连接本机 HTTP。令牌只保存在当前设备的 Android Keystore 密文中。",
            color = MiuixTheme.colorScheme.onBackgroundVariant,
            fontSize = 13.sp,
        )
    }
}

@Composable
private fun MainShell(state: MainUiState, viewModel: MainViewModel) {
    val title = when (state.screen) {
        Screen.Overview -> "概览"
        Screen.Sites -> "站点"
        Screen.SiteEditor -> if (state.draft.id == null) "新增站点" else "编辑站点"
        Screen.Checkins -> "签到历史"
        Screen.Announcements -> "公告"
    }
    Column(
        modifier = Modifier.fillMaxSize().safeDrawingPadding().background(MiuixTheme.colorScheme.background),
    ) {
        Header(
            title = title,
            onRefresh = viewModel::refresh,
            onDisconnect = viewModel::disconnect,
            busy = state.inFlight.contains("refresh"),
        )
        if (!state.error.isNullOrBlank()) {
            ErrorBanner(state.error, Modifier.padding(horizontal = 16.dp))
            Spacer(Modifier.height(8.dp))
        }
        Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
            when (state.screen) {
                Screen.Overview -> OverviewScreen(state)
                Screen.Sites -> SitesScreen(state, viewModel)
                Screen.SiteEditor -> SiteEditorScreen(state, viewModel)
                Screen.Checkins -> CheckinsScreen(state.checkins)
                Screen.Announcements -> AnnouncementsScreen(state, viewModel)
            }
        }
        BottomNavigation(state.screen, viewModel::navigate)
    }
}

@Composable
private fun Header(title: String, onRefresh: () -> Unit, onDisconnect: () -> Unit, busy: Boolean) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            ApiText("APIHub", color = MiuixTheme.colorScheme.primary, fontWeight = FontWeight.Bold)
            ApiText(title, fontSize = 24.sp, fontWeight = FontWeight.Bold)
        }
        ApiSecondaryButton(text = if (busy) "刷新中" else "刷新", onClick = onRefresh, enabled = !busy)
        Spacer(Modifier.width(6.dp))
        ApiSecondaryButton(text = "断开", onClick = onDisconnect)
    }
}

@Composable
private fun BottomNavigation(screen: Screen, onNavigate: (Screen) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 8.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        NavigationButton("概览", screen == Screen.Overview) { onNavigate(Screen.Overview) }
        NavigationButton("站点", screen == Screen.Sites || screen == Screen.SiteEditor) { onNavigate(Screen.Sites) }
        NavigationButton("签到", screen == Screen.Checkins) { onNavigate(Screen.Checkins) }
        NavigationButton("公告", screen == Screen.Announcements) { onNavigate(Screen.Announcements) }
    }
}

@Composable
private fun NavigationButton(label: String, selected: Boolean, onClick: () -> Unit) {
    if (selected) ApiPrimaryButton(label, onClick) else ApiSecondaryButton(label, onClick)
}

@Composable
private fun OverviewScreen(state: MainUiState) {
    val summary = state.summary
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (summary == null) {
            EmptyState("暂无概览数据")
            return@Column
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
            MetricCard("站点总数", summary.sites.total.toString(), Modifier.weight(1f))
            MetricCard("已启用", summary.sites.enabled.toString(), Modifier.weight(1f))
        }
        MetricCard("未读公告", summary.unreadAnnouncements.toString(), Modifier.fillMaxWidth())
        ApiCard(Modifier.fillMaxWidth()) {
            ApiText("今日签到", fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(10.dp))
            if (summary.today.isEmpty()) {
                ApiText("暂无签到记录", color = MiuixTheme.colorScheme.onBackgroundVariant)
            } else {
                summary.today.toSortedMap().forEach { (status, count) ->
                    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                        ApiText(statusLabel(status))
                        ApiText(count.toString(), fontWeight = FontWeight.Bold)
                    }
                }
            }
        }
    }
}

@Composable
private fun MetricCard(label: String, value: String, modifier: Modifier = Modifier) {
    ApiCard(modifier) {
        ApiText(label, color = MiuixTheme.colorScheme.onBackgroundVariant, fontSize = 13.sp)
        Spacer(Modifier.height(4.dp))
        ApiText(value, fontSize = 28.sp, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun SitesScreen(state: MainUiState, viewModel: MainViewModel) {
    Column(modifier = Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), horizontalArrangement = Arrangement.End) {
            ApiPrimaryButton("新增站点", viewModel::newSite)
        }
        Spacer(Modifier.height(4.dp))
        if (state.sites.isEmpty()) {
            EmptyState("还没有站点")
        } else {
            Column(
                modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                state.sites.forEach { site -> SiteCard(site, state, viewModel) }
            }
        }
    }
}

@Composable
private fun SiteCard(site: Site, state: MainUiState, viewModel: MainViewModel) {
    ApiCard(Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                ApiText(site.name, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(3.dp))
                ApiText(site.baseUrl, color = MiuixTheme.colorScheme.onBackgroundVariant, maxLines = 1)
            }
            ApiSwitch(site.enabled) { viewModel.toggleSite(site) }
        }
        Spacer(Modifier.height(8.dp))
        Row(
            modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            ApiSecondaryButton("编辑", { viewModel.editSite(site) }, enabled = !busy(state, "toggle:${site.id}"))
            if (site.capabilities.checkin && site.checkinEnabled) {
                ApiSecondaryButton(
                    "签到",
                    { viewModel.runCheckin(site) },
                    enabled = !busy(state, "checkin:${site.id}"),
                )
            }
            if (site.capabilities.announcements && site.announcementEnabled) {
                ApiSecondaryButton(
                    "同步公告",
                    { viewModel.syncAnnouncements(site) },
                    enabled = !busy(state, "sync:${site.id}"),
                )
            }
        }
        Spacer(Modifier.height(6.dp))
        ApiText(
            "${site.adapter} · ${if (site.credentialConfigured) "令牌已配置" else "令牌未配置"} · 失败 ${site.consecutiveFailures} 次",
            color = MiuixTheme.colorScheme.onBackgroundVariant,
            fontSize = 12.sp,
        )
    }
}

@Composable
private fun SiteEditorScreen(state: MainUiState, viewModel: MainViewModel) {
    val draft = state.draft
    val selectedAdapter = state.adapters.firstOrNull { it.name == draft.adapter }
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        ApiTextField(draft.name, { value -> viewModel.updateDraft { it.copy(name = value) } }, "名称", Modifier.fillMaxWidth())
        ApiTextField(draft.baseUrl, { value -> viewModel.updateDraft { it.copy(baseUrl = value) } }, "站点地址", Modifier.fillMaxWidth())
        AdapterSelector(draft.adapter, state.adapters, viewModel::selectAdapter)
        if (draft.adapter == "auto" || selectedAdapter?.capabilities?.requiresUserId != false) {
            ApiTextField(draft.userId, { value -> viewModel.updateDraft { it.copy(userId = value) } }, "用户 ID（New API 必填）", Modifier.fillMaxWidth())
        }
        ApiTextField(
            value = draft.accessToken,
            onValueChange = { value -> viewModel.updateDraft { it.copy(accessToken = value) } },
            label = if (draft.id == null) "访问令牌" else "访问令牌（留空则保留原值）",
            modifier = Modifier.fillMaxWidth(),
            visualTransformation = PasswordVisualTransformation(),
        )
        ToggleRow("启用站点", draft.enabled) { value -> viewModel.updateDraft { it.copy(enabled = value) } }
        when {
            draft.adapter == "auto" -> ApiText("自动识别后由服务器采用对应能力默认值", color = MiuixTheme.colorScheme.onBackgroundVariant)
            selectedAdapter?.capabilities?.checkin == true -> ToggleRow("启用签到", draft.checkinEnabled) { value ->
                viewModel.updateDraft { it.copy(checkinEnabled = value) }
            }
            else -> ApiText("此站点类型不支持签到", color = MiuixTheme.colorScheme.onBackgroundVariant)
        }
        when {
            draft.adapter == "auto" -> Unit
            selectedAdapter?.capabilities?.announcements == true -> ToggleRow("启用公告", draft.announcementEnabled) { value ->
                viewModel.updateDraft { it.copy(announcementEnabled = value) }
            }
            else -> ApiText("此站点类型不支持公告同步", color = MiuixTheme.colorScheme.onBackgroundVariant)
        }
        ApiTextField(draft.checkinCron, { value -> viewModel.updateDraft { it.copy(checkinCron = value) } }, "签到 Cron", Modifier.fillMaxWidth())
        ApiTextField(draft.announcementCron, { value -> viewModel.updateDraft { it.copy(announcementCron = value) } }, "公告 Cron", Modifier.fillMaxWidth())
        ApiTextField(draft.timezone, { value -> viewModel.updateDraft { it.copy(timezone = value) } }, "时区", Modifier.fillMaxWidth())
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ApiSecondaryButton("取消", { viewModel.navigate(Screen.Sites) }, Modifier.weight(1f))
            ApiPrimaryButton(
                text = if (busy(state, "save-site")) "保存中" else "保存",
                onClick = viewModel::saveSite,
                modifier = Modifier.weight(1f),
                enabled = !busy(state, "save-site"),
            )
        }
    }
}

@Composable
private fun AdapterSelector(
    selected: String,
    adapters: List<AdapterDescriptor>,
    onSelect: (String) -> Unit,
) {
    ApiText("站点类型", fontWeight = FontWeight.Bold)
    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        AdapterButton("自动识别", "auto", selected, onSelect)
        adapters.forEach { adapter ->
            AdapterButton(adapter.displayName, adapter.name, selected, onSelect)
        }
    }
}

@Composable
private fun AdapterButton(label: String, value: String, selected: String, onSelect: (String) -> Unit) {
    if (value == selected) {
        ApiPrimaryButton(label, { onSelect(value) })
    } else {
        ApiSecondaryButton(label, { onSelect(value) })
    }
}

@Composable
private fun ToggleRow(label: String, checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        ApiText(label)
        ApiSwitch(checked, onCheckedChange)
    }
}

@Composable
private fun CheckinsScreen(checkins: List<CheckinRun>) {
    if (checkins.isEmpty()) {
        EmptyState("暂无签到历史")
        return
    }
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        checkins.forEach { run ->
            ApiCard(Modifier.fillMaxWidth()) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    ApiText(run.siteName.ifBlank { run.siteId }, fontWeight = FontWeight.Bold)
                    StatusPill(statusLabel(run.status), statusColor(run.status))
                }
                Spacer(Modifier.height(5.dp))
                ApiText("${run.localDate} · ${run.message}")
                if (run.rewardValue != null) {
                    ApiText("奖励 ${run.rewardValue}", color = MiuixTheme.colorScheme.onSurface, fontSize = 12.sp)
                }
                if (!run.errorCode.isNullOrBlank()) {
                    ApiText("错误码 ${run.errorCode}", color = LocalSemanticColors.current.danger, fontSize = 12.sp)
                }
                ApiText("尝试 ${run.attemptCount} 次 · ${formatApiTime(run.finishedAt ?: run.startedAt)}", color = MiuixTheme.colorScheme.onBackgroundVariant, fontSize = 12.sp)
            }
        }
    }
}

@Composable
private fun AnnouncementsScreen(state: MainUiState, viewModel: MainViewModel) {
    if (state.announcements.isEmpty()) {
        EmptyState("暂无公告")
        return
    }
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        state.announcements.forEach { announcement ->
            AnnouncementCard(announcement, state, viewModel)
        }
    }
}

@Composable
private fun AnnouncementCard(announcement: Announcement, state: MainUiState, viewModel: MainViewModel) {
    val unread = announcement.readAt == null
    ApiCard(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            ApiText(announcement.siteName.ifBlank { announcement.siteId }, fontWeight = FontWeight.Bold)
            StatusPill(if (unread) "未读" else "已读", if (unread) LocalSemanticColors.current.info else MiuixTheme.colorScheme.onBackgroundVariant)
        }
        Spacer(Modifier.height(8.dp))
        ApiText(announcement.content, maxLines = 12)
        if (!announcement.extra.isNullOrBlank()) {
            Spacer(Modifier.height(6.dp))
            ApiText(announcement.extra, color = MiuixTheme.colorScheme.onBackgroundVariant, fontSize = 12.sp)
        }
        Spacer(Modifier.height(8.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            ApiText(formatApiTime(announcement.publishedAt ?: announcement.firstSeenAt), color = MiuixTheme.colorScheme.onBackgroundVariant, fontSize = 12.sp)
            ApiSecondaryButton(
                text = if (unread) "标记已读" else "标记未读",
                onClick = { viewModel.setAnnouncementRead(announcement, unread) },
                enabled = !busy(state, "announcement:${announcement.id}"),
            )
        }
    }
}

@Composable
private fun StatusPill(text: String, color: Color) {
    Box(
        modifier = Modifier.clip(RoundedCornerShape(6.dp)).background(color.copy(alpha = 0.15f)).padding(horizontal = 8.dp, vertical = 4.dp),
    ) {
        ApiText(text, color = MiuixTheme.colorScheme.onSurface, fontSize = 12.sp, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun EmptyState(text: String) {
    Box(Modifier.fillMaxSize().padding(32.dp), contentAlignment = Alignment.Center) {
        ApiText(text, color = MiuixTheme.colorScheme.onBackgroundVariant)
    }
}

@Composable
private fun ErrorBanner(text: String, modifier: Modifier = Modifier) {
    ApiCard(modifier.fillMaxWidth()) {
        ApiText(text, color = LocalSemanticColors.current.danger)
    }
}

private fun busy(state: MainUiState, key: String): Boolean = key in state.inFlight

private fun statusLabel(status: String): String = when (status) {
    "success" -> "成功"
    "already_checked" -> "已签到"
    "manual_required" -> "需手动"
    "failed" -> "失败"
    "running" -> "执行中"
    "skipped" -> "已跳过"
    else -> status
}

@Composable
private fun statusColor(status: String): Color = when (status) {
    "success", "already_checked" -> LocalSemanticColors.current.success
    "manual_required", "running", "skipped" -> LocalSemanticColors.current.warning
    "failed" -> LocalSemanticColors.current.danger
    else -> LocalSemanticColors.current.info
}

private val apiTimeFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm", Locale.ROOT)

internal fun formatApiTime(value: String, zoneId: ZoneId = ZoneId.systemDefault()): String = try {
    apiTimeFormatter.withZone(zoneId).format(Instant.parse(value))
} catch (_: DateTimeException) {
    value
}
