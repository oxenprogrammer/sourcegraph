import React, { FunctionComponent, useState } from 'react'

import { defaultDurationValues } from './shared'

export interface DurationSelectProps {
    id: string
    value: string | null
    disabled: boolean
    onChange?: (value: number | null) => void
    durationValues?: { value: number; displayText: string }[]
}

const defaultCustomValue = 24

const toInt = (value: string): number | null => Math.floor(parseInt(value, 10)) || null

export const DurationSelect: FunctionComponent<DurationSelectProps> = ({
    id,
    value,
    disabled,
    onChange,
    durationValues = defaultDurationValues,
}) => {
    const [isCustom, setIsCustom] = useState(
        value !== null && !durationValues.map(({ value }) => value).includes(toInt(value))
    )

    return (
        <div className="input-group">
            <select
                id={id}
                className="form-control"
                value={isCustom ? 'custom' : value || undefined}
                disabled={disabled}
                onChange={event => {
                    if (event.target.value === 'custom') {
                        setIsCustom(true)
                    } else {
                        setIsCustom(false)
                        onChange?.(toInt(event.target.value))
                    }
                }}
            >
                {durationValues.map(({ value, displayText }) => (
                    <option key={value} value={value || undefined}>
                        {displayText}
                    </option>
                ))}

                <option value="custom">Custom</option>
            </select>

            {isCustom && (
                <>
                    <input
                        type="number"
                        className="form-control ml-2"
                        value={value || defaultCustomValue}
                        min="1"
                        max="219150"
                        disabled={disabled}
                        onChange={event => onChange?.(toInt(event.target.value))}
                    />

                    <div className="input-group-append">
                        <span className="input-group-text"> hours </span>
                    </div>
                </>
            )}
        </div>
    )
}
