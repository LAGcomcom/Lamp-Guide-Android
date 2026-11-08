package com.lampguide.lampnative.ui

import com.lampguide.lampnative.data.UiError

/**
 * 每屏都要有的四态。**四态是硬要求**：加载中 / 有数据 / 空 / 错。
 * 错态永远带着网关原话（[UiError.message]），不翻译成「操作失败」。
 */
sealed interface Load<out T> {
    data object Loading : Load<Nothing>
    data class Data<T>(val value: T) : Load<T>
    data object Empty : Load<Nothing>
    data class Error(val err: UiError) : Load<Nothing>
}

fun <T, R> Load<T>.map(transform: (T) -> R): Load<R> = when (this) {
    is Load.Data -> Load.Data(transform(value))
    is Load.Empty -> Load.Empty
    is Load.Error -> this
    Load.Loading -> Load.Loading
}
