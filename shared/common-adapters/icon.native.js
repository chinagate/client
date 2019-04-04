// @flow
import * as React from 'react'
import * as Shared from './icon.shared'
import * as Styles from '../styles'
import ClickableBox from './clickable-box'
import logger from '../logger'
import type {IconType, Props} from './icon'
import {NativeImage} from './native-image.native'
import {NativeText} from './native-wrappers.native'
import {iconMeta} from './icon.constants'

const Kb = {
  ClickableBox,
  NativeImage,
  NativeText,
}

// 1. remove emotion
// 2. make defaults ahead of time
//

const Text = React.memo(p => {
  const style = {}

  const color =
    p.colorOverride ||
    p.color ||
    (p.style && p.style.color) ||
    Shared.defaultColor(p.type) ||
    (p.opacity && Styles.globalColors.lightGrey)

  if (p.style) {
    if (p.style.width !== undefined) {
      style.width = p.style.width
    }
    if (p.style.backgroundColor) {
      style.backgroundColor = p.style.backgroundColor
    }
  }

  if (color) {
    style.color = color
  }
  if (p.textAlign !== undefined) {
    style.textAlign = p.textAlign
  }

  if (p.fontSize !== undefined || (p.style && p.style.width !== undefined)) {
    style.fontSize = p.fontSize || p.style.width
  }

  const temp = Shared.fontSize(Shared.typeToIconMapper(p.type))
  if (temp) {
    style.fontSize = temp.fontSize
  }

  // explicit
  let fontSize
  if (p.fontSize) {
    fontSize = p.fontSize
  } else {
    fontSize = Shared.typeToFontSize(p.sizeType)
  }

  return (
    <Kb.NativeText
      style={[styles.text, style, p.style]}
      allowFontScaling={false}
      type={p.type}
      fontSize={fontSize}
      onPress={p.onClick}
    >
      {p.children}
    </Kb.NativeText>
  )
})

const Image = React.memo(p => {
  let style
  if (p.style) {
    style = {}
    if (p.style.width !== undefined) {
      style.width = p.style.width
    }
    if (p.style.height !== undefined) {
      style.height = p.style.height
    }
    if (p.style.backgroundColor) {
      style.backgroundColor = p.style.backgroundColor
    }
  }

  return <Kb.NativeImage style={[style, p.style]} source={p.source} resizeMode="contain" />
})

class Icon extends React.PureComponent<Props> {
  static defaultProps = {
    sizeType: 'Default',
  }
  render() {
    const props = this.props
    // Only apply props.style to icon if there is no onClick
    const hasContainer = props.onClick && props.style
    let iconType = Shared.typeToIconMapper(props.type)

    if (!iconType) {
      logger.warn('Null iconType passed')
      return null
    }
    if (!iconMeta[iconType]) {
      logger.warn(`Invalid icon type passed in: ${iconType}`)
      return null
    }

    let icon

    if (iconMeta[iconType].isFont) {
      const code = String.fromCharCode(iconMeta[iconType].charCode || 0)
      let color
      if (props.colorOverride || props.color) {
        color = props.colorOverride || props.color
      }

      icon = (
        <Text
          style={hasContainer ? null : props.style}
          color={color}
          type={props.type}
          fontSize={this.props.fontSize}
          sizeType={this.props.sizeType}
          onClick={props.onClick}
        >
          {code}
        </Text>
      )
    } else {
      icon = <Image source={iconMeta[iconType].require} style={hasContainer ? null : props.style} />
    }

    return !props.noContainer && props.onClick ? (
      <Kb.ClickableBox
        activeOpacity={0.8}
        underlayColor={props.underlayColor || Styles.globalColors.white}
        onClick={props.onClick}
        style={Styles.collapseStyles([
          props.style,
          this.props.padding && Shared.paddingStyles[this.props.padding],
        ])}
      >
        {icon}
      </Kb.ClickableBox>
    ) : (
      icon
    )
  }
}

export function iconTypeToImgSet(imgMap: {[size: string]: IconType}, targetSize: number): any {
  const multsMap = Shared.getMultsMap(imgMap, targetSize)
  const idealMults = [2, 3, 1]
  for (let mult of idealMults) {
    if (multsMap[mult]) {
      return iconMeta[imgMap[multsMap[mult]]].require
    }
  }
  return null
  // Ideally it'd do this but RN won't let you specify multiple required() sources and give it a multplier, it really
  // wants @2x etc. So instead of this we'll just supply 2x
  // return Object.keys(multsMap).map(mult => ({
  // height: parseInt(mult, 10) * targetSize,
  // uri: iconMeta[imgMap[multsMap[mult]]].require,
  // width: parseInt(mult, 10) * targetSize,
  // }))
}

export function urlsToImgSet(imgMap: {[size: string]: string}, targetSize: number): any {
  const multsMap = Shared.getMultsMap(imgMap, targetSize)
  const imgSet = Object.keys(multsMap)
    .map(mult => {
      const uri = imgMap[multsMap[mult]]
      if (!uri) {
        return null
      }
      return {
        height: parseInt(mult, 10) * targetSize,
        uri,
        width: parseInt(mult, 10) * targetSize,
      }
    })
    .filter(Boolean)
  return imgSet.length ? imgSet : null
}

export function castPlatformStyles(styles: any) {
  return Shared.castPlatformStyles(styles)
}

const styles = Styles.styleSheetCreate({
  text: {
    color: Styles.globalColors.black_50, // MUST set this or it can be inherited from outside text
    fontFamily: 'kb',
    fontWeight: 'normal', // MUST set this or it can be inherited from outside text
  },
})

export default Icon
