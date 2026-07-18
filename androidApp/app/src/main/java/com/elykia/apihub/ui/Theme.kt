package com.elykia.apihub.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import top.yukonga.miuix.kmp.basic.ButtonDefaults
import top.yukonga.miuix.kmp.basic.Card
import top.yukonga.miuix.kmp.basic.Text
import top.yukonga.miuix.kmp.basic.TextButton
import top.yukonga.miuix.kmp.basic.TextField
import top.yukonga.miuix.kmp.basic.Switch
import top.yukonga.miuix.kmp.theme.MiuixTheme
import top.yukonga.miuix.kmp.theme.darkColorScheme
import top.yukonga.miuix.kmp.theme.lightColorScheme

internal const val LIGHT_BRAND_BLUE_ARGB = 0xFF163BF2
internal const val LIGHT_BRAND_TEXT_ARGB = 0xFFFFFFFF
internal const val LIGHT_ACCENT_ARGB = 0xFF7A60D2
internal const val LIGHT_SUCCESS_ARGB = 0xFF57BD6A
internal const val LIGHT_WARNING_ARGB = 0xFFC28B00
internal const val LIGHT_DANGER_ARGB = 0xFFD80020
internal const val LIGHT_INFO_ARGB = 0xFF3E86F6
internal const val LIGHT_SURFACE_ARGB = 0xFFFFFFFF
internal const val LIGHT_ON_SURFACE_ARGB = 0xFF172035
internal const val DARK_BRAND_GOLD_ARGB = 0xFFF5B82A
internal const val DARK_BRAND_TEXT_ARGB = 0xFF1A1508
internal const val DARK_ACCENT_ARGB = 0xFFA78BFA
internal const val DARK_SUCCESS_ARGB = 0xFF3E9F50
internal const val DARK_WARNING_ARGB = 0xFFFFC93E
internal const val DARK_ERROR_ARGB = 0xFFFF3842
internal const val DARK_INFO_ARGB = 0xFF0084FF
private val LightBrandBlue = Color(LIGHT_BRAND_BLUE_ARGB)
private val LightBrandBlueText = Color(LIGHT_BRAND_TEXT_ARGB)
private val LightAccent = Color(LIGHT_ACCENT_ARGB)
private val DarkBrandGold = Color(DARK_BRAND_GOLD_ARGB)
private val DarkBrandText = Color(DARK_BRAND_TEXT_ARGB)
private val DarkError = Color(DARK_ERROR_ARGB)
private val DarkAccent = Color(DARK_ACCENT_ARGB)

@Immutable
data class SemanticColors(
    val success: Color,
    val warning: Color,
    val danger: Color,
    val info: Color,
)

val LocalSemanticColors = staticCompositionLocalOf {
    SemanticColors(
        success = Color(LIGHT_SUCCESS_ARGB),
        warning = Color(LIGHT_WARNING_ARGB),
        danger = Color(LIGHT_DANGER_ARGB),
        info = Color(LIGHT_INFO_ARGB),
    )
}

@Composable
fun APIHubTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (dark) {
        darkColorScheme(
            primary = DarkBrandGold,
            onPrimary = DarkBrandText,
            primaryVariant = DarkAccent,
            onPrimaryVariant = DarkBrandText,
            error = DarkError,
            onError = DarkBrandText,
            background = Color(0xFF100D08),
            onBackground = Color(0xFFF5F1E8),
            surface = Color(0xFF1A1508),
            onSurface = Color(0xFFF5F1E8),
            surfaceContainer = Color(0xFF211C0E),
            onSurfaceContainer = Color(0xFFF5F1E8),
            onSecondaryVariant = Color(0xFFF5F1E8),
        )
    } else {
        lightColorScheme(
            primary = LightBrandBlue,
            onPrimary = LightBrandBlueText,
            primaryVariant = LightAccent,
            onPrimaryVariant = LightBrandBlueText,
            error = Color(0xFFD80020),
            onError = Color.White,
            background = Color(0xFFF7F8FF),
            onBackground = Color(0xFF172035),
            surface = Color(LIGHT_SURFACE_ARGB),
            onSurface = Color(LIGHT_ON_SURFACE_ARGB),
            surfaceContainer = Color(LIGHT_SURFACE_ARGB),
            onSurfaceContainer = Color(LIGHT_ON_SURFACE_ARGB),
            onSecondaryVariant = Color(LIGHT_ON_SURFACE_ARGB),
        )
    }
    val semantic = if (dark) {
        SemanticColors(Color(DARK_SUCCESS_ARGB), Color(DARK_WARNING_ARGB), DarkError, Color(DARK_INFO_ARGB))
    } else {
        SemanticColors(Color(LIGHT_SUCCESS_ARGB), Color(LIGHT_WARNING_ARGB), Color(LIGHT_DANGER_ARGB), Color(LIGHT_INFO_ARGB))
    }
    androidx.compose.runtime.CompositionLocalProvider(LocalSemanticColors provides semantic) {
        MiuixTheme(colors = colors, content = content)
    }
}

@Composable
fun ApiText(
    text: String,
    modifier: Modifier = Modifier,
    color: Color = Color.Unspecified,
    fontSize: TextUnit = TextUnit.Unspecified,
    fontWeight: FontWeight? = null,
    maxLines: Int = Int.MAX_VALUE,
) {
    Text(
        text = text,
        modifier = modifier,
        color = color,
        fontSize = fontSize,
        fontWeight = fontWeight,
        maxLines = maxLines,
    )
}

@Composable
fun ApiCard(modifier: Modifier = Modifier, content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
    Card(
        modifier = modifier,
        insideMargin = PaddingValues(16.dp),
        content = content,
    )
}

@Composable
fun ApiPrimaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    TextButton(
        text = text,
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        colors = ButtonDefaults.textButtonColorsPrimary(),
    )
}

@Composable
fun ApiSecondaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    TextButton(text = text, onClick = onClick, modifier = modifier, enabled = enabled)
}

@Composable
fun ApiTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    singleLine: Boolean = true,
    visualTransformation: VisualTransformation = VisualTransformation.None,
) {
    TextField(
        value = value,
        onValueChange = onValueChange,
        label = label,
        modifier = modifier,
        singleLine = singleLine,
        visualTransformation = visualTransformation,
    )
}

@Composable
fun ApiSwitch(checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    Switch(checked = checked, onCheckedChange = onCheckedChange)
}
