package com.elykia.apihub.ui

import com.google.common.truth.Truth.assertThat
import java.time.ZoneId
import org.junit.Test
import kotlin.math.pow

class ThemeTest {
    @Test
    fun anheyuBrandBlueTokensStayPinned() {
        assertThat(LIGHT_BRAND_BLUE_ARGB).isEqualTo(0xFF163BF2)
        assertThat(LIGHT_BRAND_TEXT_ARGB).isEqualTo(0xFFFFFFFF)
        assertThat(LIGHT_ACCENT_ARGB).isEqualTo(0xFF7A60D2)
        assertThat(DARK_BRAND_GOLD_ARGB).isEqualTo(0xFFF5B82A)
        assertThat(DARK_BRAND_TEXT_ARGB).isEqualTo(0xFF1A1508)
        assertThat(DARK_ACCENT_ARGB).isEqualTo(0xFFA78BFA)
        assertThat(listOf(LIGHT_SUCCESS_ARGB, LIGHT_WARNING_ARGB, LIGHT_DANGER_ARGB, LIGHT_INFO_ARGB))
            .containsExactly(0xFF57BD6A, 0xFFC28B00, 0xFFD80020, 0xFF3E86F6)
            .inOrder()
        assertThat(listOf(DARK_SUCCESS_ARGB, DARK_WARNING_ARGB, DARK_ERROR_ARGB, DARK_INFO_ARGB))
            .containsExactly(0xFF3E9F50, 0xFFFFC93E, 0xFFFF3842, 0xFF0084FF)
            .inOrder()
    }

    @Test
    fun textColorsUsedOnCardsMeetWcagAaContrast() {
        assertThat(contrast(LIGHT_ON_SURFACE_ARGB, LIGHT_SURFACE_ARGB)).isAtLeast(4.5)
        assertThat(contrast(LIGHT_DANGER_ARGB, LIGHT_SURFACE_ARGB)).isAtLeast(4.5)
        assertThat(contrast(DARK_BRAND_TEXT_ARGB, DARK_ERROR_ARGB)).isAtLeast(4.5)
    }

    @Test
    fun apiTimestampsRenderInTheDeviceTimezone() {
        assertThat(formatApiTime("2026-07-18T04:00:00.000Z", ZoneId.of("Asia/Shanghai")))
            .isEqualTo("2026-07-18 12:00")
        assertThat(formatApiTime("not-a-time", ZoneId.of("UTC"))).isEqualTo("not-a-time")
    }

    private fun contrast(first: Long, second: Long): Double {
        val brighter = maxOf(luminance(first), luminance(second))
        val darker = minOf(luminance(first), luminance(second))
        return (brighter + 0.05) / (darker + 0.05)
    }

    private fun luminance(argb: Long): Double {
        val channels = listOf(16, 8, 0).map { shift ->
            val channel = ((argb shr shift) and 0xff).toDouble() / 255.0
            if (channel <= 0.04045) channel / 12.92 else ((channel + 0.055) / 1.055).pow(2.4)
        }
        return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
    }
}
