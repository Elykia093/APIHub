package com.elykia.apihub.ui

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class MiuixComponentsTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun primaryButtonIsReachableAndClickable() {
        var clicked = false
        composeRule.setContent {
            APIHubTheme {
                ApiPrimaryButton(text = "连接", onClick = { clicked = true })
            }
        }

        composeRule.onNodeWithText("连接").assertIsEnabled().performClick()

        assertTrue(clicked)
    }
}
